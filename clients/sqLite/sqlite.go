package sqlite

import (
	"encoding/json"
	"fmt"
	"os"
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

var db map[string]UserData

func New(path string) *Store {
	return &Store{
		path: path,
	}
}

func (s *Store) loadUsers() {
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

func (s *Store) saveUsers(users map[string]UserData) error {
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return err
	}

	// update package-level db with a copy to avoid aliasing external map
	newDB := make(map[string]UserData, len(users))
	for k, v := range users {
		newDB[k] = v
	}
	db = newDB

	return nil
}

func (s *Store) AddDays(userID string, days int64) error {
	s.loadUsers()

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

	return s.saveUsers(db)
}

func (s *Store) GetDays(userID string) (int64, error) {
	s.loadUsers()

	userData, exist := db[userID]

	if exist {
		return userData.Days, nil
	} else {
		return 0, colorfulprint.PrintError(fmt.Sprintf("userid(%s) does not exist in DataBase", userID), nil)
	}
}

func (s *Store) GetCertRef(userID string) (string, error) {
	s.loadUsers()

	userData, exist := db[userID]

	if exist {
		return userData.CertRef, nil
	} else {
		return "", colorfulprint.PrintError(fmt.Sprintf("userid(%s) does not exist in DataBase", userID), nil)
	}
}

func (s *Store) DeductDay(userID string) error {
	s.loadUsers()

	userData, exist := db[userID]
	if !exist {
		return fmt.Errorf("user %s not found", userID)
	}

	if userData.Days > 0 {
		userData.Days--
	}
	userData.LastDeduct = time.Now().UTC().Format(time.RFC3339)
	db[userID] = userData

	return s.saveUsers(db)
}

func (s *Store) GetAllUsers() map[string]UserData {
	s.loadUsers()
	result := make(map[string]UserData)
	for k, v := range db {
		result[k] = v
	}
	return result
}

// SetCertRef сохраняет или обновляет certRef для пользователя,
// не изменяя Days и корректно инициализируя запись при необходимости.
func (s *Store) SetCertRef(userID, certRef string) error {
	s.loadUsers()

	ud, ok := db[userID]
	if !ok {
		ud = UserData{
			Days:       0,
			LastDeduct: time.Now().UTC().Format(time.RFC3339),
		}
	}
	ud.CertRef = certRef
	db[userID] = ud
	return s.saveUsers(db)
}
