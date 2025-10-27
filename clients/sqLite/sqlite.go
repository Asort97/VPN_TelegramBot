package sqlite

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	colorfulprint "github.com/Asort97/vpnBot/clients/colorfulPrint"
)

type Store struct {
	path string
}

type UserData struct {
	Days       int64  `json:"days"`
	CertRef    string `json:"certref"`
	LastDeduct string `json:"last_deduct"` // ISO8601 timestamp
}

var (
	db   map[string]UserData
	dbMu sync.Mutex
)

func New(path string) *Store {
	return &Store{
		path: path,
	}
}

func (s *Store) loadUsersLocked() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// file doesn't exist yet — initialize empty DB
			db = make(map[string]UserData)
			return
		}
		// other read errors: keep db nil/empty
		return
	}

	if len(data) == 0 {
		db = make(map[string]UserData)
		return
	}

	var tmp map[string]UserData
	if err := json.Unmarshal(data, &tmp); err != nil {
		// invalid JSON — initialize empty DB (could also choose to preserve existing)
		db = make(map[string]UserData)
		return
	}
	db = tmp
}

func (s *Store) saveUsersLocked() error {
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return err
	}

	return nil
}

func (s *Store) AddDays(userID string, days int64) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	s.loadUsersLocked()

	now := time.Now().UTC()
	userData, exist := db[userID]

	if !exist {
		userData = UserData{
			Days:       days,
			LastDeduct: now.Format(time.RFC3339),
		}
	} else {
		prev := userData.Days
		userData.Days += days
		// если пополнение было с нуля -> начать новый 24ч цикл от момента пополнения
		if prev == 0 && userData.Days > 0 {
			userData.LastDeduct = now.Format(time.RFC3339)
		}
	}

	db[userID] = userData

	return s.saveUsersLocked()
}

func (s *Store) GetDays(userID string) (int64, error) {
	dbMu.Lock()
	defer dbMu.Unlock()

	s.loadUsersLocked()

	userData, exist := db[userID]

	if exist {
		return userData.Days, nil
	} else {
		return 0, colorfulprint.PrintError(fmt.Sprintf("userid(%s) does not exist in DataBase", userID), nil)
	}
}

func (s *Store) GetCertRef(userID string) (string, error) {
	dbMu.Lock()
	defer dbMu.Unlock()

	s.loadUsersLocked()

	userData, exist := db[userID]

	if exist {
		return userData.CertRef, nil
	} else {
		return "", colorfulprint.PrintError(fmt.Sprintf("userid(%s) does not exist in DataBase", userID), nil)
	}
}

func (s *Store) ConsumeDays(userID string, days int64, nextCheck time.Time) (int64, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days to consume must be positive")
	}

	dbMu.Lock()
	defer dbMu.Unlock()

	s.loadUsersLocked()

	userData, exist := db[userID]
	if !exist {
		return 0, fmt.Errorf("user %s not found", userID)
	}

	if userData.Days <= 0 {
		return userData.Days, nil
	}

	if days > userData.Days {
		days = userData.Days
	}

	userData.Days -= days
	if nextCheck.IsZero() {
		nextCheck = time.Now().UTC()
	} else {
		nextCheck = nextCheck.UTC()
	}
	userData.LastDeduct = nextCheck.Format(time.RFC3339)
	db[userID] = userData

	if err := s.saveUsersLocked(); err != nil {
		return 0, err
	}

	return userData.Days, nil
}

func (s *Store) GetAllUsers() map[string]UserData {
	dbMu.Lock()
	defer dbMu.Unlock()

	s.loadUsersLocked()
	result := make(map[string]UserData)
	for k, v := range db {
		result[k] = v
	}
	return result
}

// SetCertRef сохраняет или обновляет certRef для пользователя,
// не изменяя Days и корректно инициализируя запись при необходимости.
func (s *Store) SetCertRef(userID, certRef string) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	s.loadUsersLocked()

	ud, ok := db[userID]
	if !ok {
		ud = UserData{
			Days:       0,
			LastDeduct: time.Now().UTC().Format(time.RFC3339),
		}
	}
	ud.CertRef = certRef
	db[userID] = ud
	return s.saveUsersLocked()
}
