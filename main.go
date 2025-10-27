package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	instruct "github.com/Asort97/vpnBot/clients/instruction"
	pfsense "github.com/Asort97/vpnBot/clients/pfSense"
	sqlite "github.com/Asort97/vpnBot/clients/sqLite"
	yookassa "github.com/Asort97/vpnBot/clients/yooKassa"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const startText = `
Привет! <b>Добро пожаловать в HappyCat VPN</b> 😺🔐

Здесь ты можешь:
• Моментально получить или продлить доступ к VPN.
• Скачать пробный сертификат в пару кликов.
• Найти подробные инструкции для всех устройств.
• Оперативно связаться с поддержкой 24/7.

Готов? Выбирай нужный раздел в меню ниже и поехали! 🚀
`

var lastActionKey = make(map[int64]map[string]time.Time)

var invoiceToken string
var invoiceTokenTest string
var yookassaClient *yookassa.YooKassaClient
var sqliteClient *sqlite.Store

type SessionState string

const (
	stateMenu         SessionState = "menu"
	stateGetVPN       SessionState = "get_vpn"
	stateTopUp        SessionState = "top_up"
	stateTrial        SessionState = "trial"
	stateStatus       SessionState = "status"
	stateSupport      SessionState = "support"
	stateInstructions SessionState = "instructions"
	stateChooseRate   SessionState = "choose_rate"
)

// RatePlan описывает тариф, который пользователь может выбрать.
type RatePlan struct {
	ID          string
	Title       string
	Amount      float64
	Days        int
	Description string
}

// ratePlans содержит список доступных тарифов. При необходимости поменяйте названия и цены.
var ratePlans = []RatePlan{
	{ID: "15d", Title: "15 дней", Amount: 25, Days: 15, Description: "Идеально, чтобы протестировать сервис или уехать на короткое время."},
	{ID: "30d", Title: "30 дней", Amount: 50, Days: 30, Description: "Идеально, чтобы протестировать сервис или уехать на короткое время."},
	{ID: "60d", Title: "60 дней", Amount: 100, Days: 60, Description: "Базовая подписка для постоянного доступа без ограничений."},
	{ID: "120d", Title: "120 дней", Amount: 200, Days: 120, Description: "Полугодовой тариф со скидкой по сравнению с помесячной оплатой."},
	{ID: "240d", Title: "240 дней", Amount: 300, Days: 240, Description: "Полугодовой тариф со скидкой по сравнению с помесячной оплатой."},
	{ID: "365d", Title: "365 дней", Amount: 400, Days: 365, Description: "Максимальная выгода для тех, кто пользуется VPN круглый год."},
}

var ratePlanByID = func() map[string]RatePlan {
	result := make(map[string]RatePlan)
	for _, plan := range ratePlans {
		result[plan.ID] = plan
	}
	return result
}()

type userState struct {
	TelegramUser string
	UserID       string
	UserExists   bool
	CertID       string
	CertRefID    string
	CertName     string
	CertExpireAt string
	CertExpired  bool
	CertDays     int
}

func fetchUserState(pfsenseClient *pfsense.PfSenseClient, telegramUser string) (userState, error) {
	state := userState{TelegramUser: telegramUser}

	userIDStr, exists := pfsenseClient.IsUserExist(telegramUser)
	if !exists {
		return state, nil
	}

	state.UserExists = true
	state.UserID = userIDStr

	userIDFromCert, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUser)
	if userIDFromCert != "" {
		state.UserID = userIDFromCert
	}
	if certRefID == "" || err != nil {
		return state, nil
	}

	state.CertRefID = certRefID

	certID, certName, err := pfsenseClient.GetCertificateIDByRefid(certRefID)
	if err != nil {
		return state, nil
	}
	state.CertID = certID
	state.CertName = certName
	state.CertDays = extractDaysFromCertName(certName)

	_, expiresAt, _, expired, err := pfsenseClient.GetDateOfCertificate(certID)
	if err != nil {
		return state, err
	}

	state.CertExpireAt = expiresAt
	state.CertExpired = expired
	return state, nil
}

func ensureVPNUser(pfsenseClient *pfsense.PfSenseClient, telegramUser string) (string, error) {
	userIDStr, exists := pfsenseClient.IsUserExist(telegramUser)
	if exists {
		return userIDStr, nil
	}

	return pfsenseClient.CreateUser(telegramUser, "123", "", "", false)
}

const certificateLifetimeDays = 3650

func createAndAttachCertificate(pfsenseClient *pfsense.PfSenseClient, telegramUser string, userID string) (string, string, error) {
	certName := fmt.Sprintf("Cert%s_permanent", telegramUser)

	if existingRefID, existingID, err := pfsenseClient.GetCertificateIDByName(certName); err == nil {
		if err := pfsenseClient.AttachCertificateToUser(userID, existingRefID); err != nil {
			return "", "", err
		}
		_, expiresAt, _, _, err := pfsenseClient.GetDateOfCertificate(existingID)
		if err != nil {
			return "", "", err
		}
		return existingRefID, expiresAt, nil
	}

	uuid, err := pfsenseClient.GetCARef()
	if err != nil {
		return "", "", err
	}

	certID, certRefID, err := pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, certificateLifetimeDays, "", "sha256", telegramUser)
	if err != nil {
		return "", "", err
	}

	if err := pfsenseClient.AttachCertificateToUser(userID, certRefID); err != nil {
		return "", "", err
	}

	_, expiresAt, _, _, err := pfsenseClient.GetDateOfCertificate(certID)
	if err != nil {
		return "", "", err
	}

	return certRefID, expiresAt, nil
}

func ensureUserCertificate(pfsenseClient *pfsense.PfSenseClient, telegramUser string) (string, string, string, error) {
	state, err := fetchUserState(pfsenseClient, telegramUser)
	if err != nil {
		return "", "", "", err
	}

	userID := state.UserID
	if !state.UserExists || userID == "" {
		userID, err = ensureVPNUser(pfsenseClient, telegramUser)
		if err != nil {
			return "", "", "", err
		}
	}

	certRefID := state.CertRefID
	certID := state.CertID
	expiresAt := state.CertExpireAt

	if certRefID == "" {
		if ref, getErr := sqliteClient.GetCertRef(telegramUser); getErr == nil && ref != "" {
			certRefID = ref
		}
	}

	if certRefID != "" && certID == "" {
		if id, _, getErr := pfsenseClient.GetCertificateIDByRefid(certRefID); getErr == nil {
			certID = id
		} else {
			log.Printf("GetCertificateIDByRefid error: %v", getErr)
			certRefID = ""
		}
	}

	createdNew := false
	if certRefID == "" {
		certRefID, expiresAt, err = createAndAttachCertificate(pfsenseClient, telegramUser, userID)
		if err != nil {
			return "", "", "", err
		}
		createdNew = true
	} else {
		if err := pfsenseClient.AttachCertificateToUser(userID, certRefID); err != nil {
			return "", "", "", err
		}
		if expiresAt == "" {
			if certID == "" {
				if id, _, getErr := pfsenseClient.GetCertificateIDByRefid(certRefID); getErr == nil {
					certID = id
				} else {
					log.Printf("GetCertificateIDByRefid error: %v", getErr)
				}
			}
			if certID != "" {
				if _, expires, _, _, getErr := pfsenseClient.GetDateOfCertificate(certID); getErr == nil {
					expiresAt = expires
				} else {
					log.Printf("GetDateOfCertificate error: %v", getErr)
				}
			}
		}
	}

	if err := sqliteClient.SetCertRef(telegramUser, certRefID); err != nil {
		log.Printf("sqliteClient.SetCertRef error: %v", err)
	}

	if expiresAt == "" && !createdNew {
		if certID == "" {
			if id, _, getErr := pfsenseClient.GetCertificateIDByRefid(certRefID); getErr == nil {
				certID = id
			}
		}
		if certID != "" {
			if _, expires, _, _, getErr := pfsenseClient.GetDateOfCertificate(certID); getErr == nil {
				expiresAt = expires
			} else {
				log.Printf("GetDateOfCertificate error: %v", getErr)
			}
		}
	}

	return certRefID, expiresAt, userID, nil
}

func issuePlanCertificate(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, plan RatePlan, pfsenseClient *pfsense.PfSenseClient, telegramUser string, numericUserID int64) error {
	certRefID, expiresAt, _, err := ensureUserCertificate(pfsenseClient, telegramUser)
	if err != nil {
		return err
	}

	if plan.Days > 0 {
		if err := sqliteClient.AddDays(telegramUser, int64(plan.Days)); err != nil {
			log.Printf("sqliteClient.AddDays error: %v", err)
		}
	}

	if err := pfsenseClient.UnrevokeCertificate(certRefID); err != nil {
		log.Printf("Unrevoke certificate %s error: %v", certRefID, err)
	}

	if expiresAt == "" {
		if id, _, getErr := pfsenseClient.GetCertificateIDByRefid(certRefID); getErr == nil {
			if _, exp, _, _, getErr := pfsenseClient.GetDateOfCertificate(id); getErr == nil {
				expiresAt = exp
			} else {
				log.Printf("GetDateOfCertificate error: %v", getErr)
			}
		}
	}

	return sendCertificate(certRefID, telegramUser, expiresAt, false, chatID, plan.Days, numericUserID, pfsenseClient, bot, session)
}
func resolvePlanFromMetadata(meta map[string]interface{}, session *UserSession) RatePlan {
	plan := RatePlan{}

	if meta != nil {
		if v, ok := meta["plan_id"]; ok {
			id := fmt.Sprint(v)
			plan.ID = id
			if preset, ok := ratePlanByID[id]; ok {
				plan = preset
			}
		}

		if plan.Title == "" {
			if v, ok := meta["plan_title"]; ok {
				plan.Title = fmt.Sprint(v)
			} else if v, ok := meta["product"]; ok {
				plan.Title = fmt.Sprint(v)
			}
		}

		if plan.Days == 0 {
			if v, ok := meta["plan_days"]; ok {
				switch value := v.(type) {
				case float64:
					plan.Days = int(value)
				case string:
					if n, err := strconv.Atoi(value); err == nil {
						plan.Days = n
					}
				}
			}
		}

		if plan.Amount == 0 {
			if v, ok := meta["plan_amount"]; ok {
				switch value := v.(type) {
				case float64:
					plan.Amount = value
				case string:
					if n, err := strconv.ParseFloat(value, 64); err == nil {
						plan.Amount = n
					}
				}
			}
		}
	}

	if plan.ID != "" {
		if preset, ok := ratePlanByID[plan.ID]; ok {
			if plan.Title == "" {
				plan.Title = preset.Title
			}
			if plan.Days == 0 {
				plan.Days = preset.Days
			}
			if plan.Amount == 0 {
				plan.Amount = preset.Amount
			}
		}
	}

	if plan.Days == 0 && plan.Title != "" {
		plan.Days = extractDaysFromCertName(plan.Title)
	}

	if plan.Days == 0 && session != nil && session.PendingPlanID != "" {
		if preset, ok := ratePlanByID[session.PendingPlanID]; ok {
			if plan.ID == "" {
				plan.ID = preset.ID
			}
			if plan.Title == "" {
				plan.Title = preset.Title
			}
			plan.Days = preset.Days
			if plan.Amount == 0 {
				plan.Amount = preset.Amount
			}
		}
	}

	if plan.Days == 0 {
		plan.Days = 30
	}

	if plan.ID == "" && session != nil && session.PendingPlanID != "" {
		plan.ID = session.PendingPlanID
	}

	if plan.Title == "" && plan.ID != "" {
		if preset, ok := ratePlanByID[plan.ID]; ok {
			plan.Title = preset.Title
			if plan.Amount == 0 {
				plan.Amount = preset.Amount
			}
		} else {
			plan.Title = fmt.Sprintf("Пакет %s", plan.ID)
		}
	}

	if plan.Amount == 0 && plan.ID != "" {
		if preset, ok := ratePlanByID[plan.ID]; ok {
			plan.Amount = preset.Amount
		}
	}

	return plan
}

func extractDaysFromCertName(name string) int {
	if name == "" {
		return 0
	}
	idx := strings.LastIndex(name, "_")
	if idx == -1 || idx+1 >= len(name) {
		return 0
	}
	suffix := name[idx+1:]
	digits := strings.Builder{}
	for _, r := range suffix {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return 0
	}
	value, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0
	}
	return value
}

type UserSession struct {
	MessageID     int
	State         SessionState
	ContentType   string
	PendingPlanID string
}

var userSessions = make(map[int64]*UserSession)

func canProceedKey(userID int64, key string, interval time.Duration) bool {
	now := time.Now()
	if lastActionKey[userID] == nil {
		lastActionKey[userID] = make(map[string]time.Time)
	}
	if t, ok := lastActionKey[userID][key]; ok {
		if now.Sub(t) < interval {
			return false
		}
	}
	lastActionKey[userID][key] = now
	return true
}

func getSession(chatID int64) *UserSession {
	if session, ok := userSessions[chatID]; ok {
		return session
	}
	session := &UserSession{}
	userSessions[chatID] = session
	return session
}

func updateSessionText(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, state SessionState, text string, parseMode string, keyboard tgbotapi.InlineKeyboardMarkup) error {
	if session.MessageID != 0 {
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, session.MessageID, text, keyboard)
		if parseMode != "" {
			edit.ParseMode = parseMode
		}
		if _, err := bot.Send(edit); err == nil {
			instruct.ResetState(chatID)
			session.State = state
			session.ContentType = "text"
			return nil
		}
	}
	return replaceSessionWithText(bot, chatID, session, state, text, parseMode, keyboard)
}

func replaceSessionWithText(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, state SessionState, text string, parseMode string, keyboard tgbotapi.InlineKeyboardMarkup) error {
	if session.MessageID != 0 {
		_, _ = bot.Send(tgbotapi.NewDeleteMessage(chatID, session.MessageID))
	}
	instruct.ResetState(chatID)
	msg := tgbotapi.NewMessage(chatID, text)
	if parseMode != "" {
		msg.ParseMode = parseMode
	}
	msg.ReplyMarkup = keyboard

	sent, err := bot.Send(msg)
	if err != nil {
		return err
	}

	session.MessageID = sent.MessageID
	session.State = state
	session.ContentType = "text"
	return nil
}

func replaceSessionWithDocument(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, state SessionState, file tgbotapi.FileBytes, caption string, parseMode string, keyboard tgbotapi.InlineKeyboardMarkup) error {
	if session.MessageID != 0 {
		_, _ = bot.Send(tgbotapi.NewDeleteMessage(chatID, session.MessageID))
	}
	instruct.ResetState(chatID)

	doc := tgbotapi.NewDocument(chatID, file)
	doc.Caption = caption
	if parseMode != "" {
		doc.ParseMode = parseMode
	}
	doc.ReplyMarkup = keyboard

	sent, err := bot.Send(doc)
	if err != nil {
		return err
	}

	session.MessageID = sent.MessageID
	session.State = state
	session.ContentType = "document"
	return nil
}

func mainMenuInlineKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Подключить VPN", "nav_get_vpn"),
			tgbotapi.NewInlineKeyboardButtonData("Пополнить баланс", "nav_topup"),
		),
		tgbotapi.NewInlineKeyboardRow(
			// tgbotapi.NewInlineKeyboardButtonData("VPN бесплатно", "nav_trial"),
			tgbotapi.NewInlineKeyboardButtonData("Статус", "nav_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Инструкции", "nav_instructions"),
			tgbotapi.NewInlineKeyboardButtonData("Поддержка", "nav_support"),
		),
	)
}

func instructionsMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💻 Windows", "windows"),
			tgbotapi.NewInlineKeyboardButtonData("📱Android", "android"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🍎 IOS", "ios"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", "nav_menu"),
		),
	)
}

func singleBackKeyboard(target string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", target),
		),
	)
}

func rateSelectionKeyboard() tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton

	for _, plan := range ratePlans {
		label := fmt.Sprintf("%s — %.0f ₽", plan.Title, plan.Amount)
		btn := tgbotapi.NewInlineKeyboardButtonData(label, "rate_"+plan.ID)
		currentRow = append(currentRow, btn)

		// По 3 кнопки в строку
		if len(currentRow) == 3 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}

	// Добавить остаток (если есть)
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	// Кнопка "Назад" всегда на отдельной строке
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", "nav_menu"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func showRateSelection(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, intro string) error {
	session.PendingPlanID = ""
	var parts []string
	if strings.TrimSpace(intro) != "" {
		parts = append(parts, intro)
	}
	parts = append(parts, "<b>Выберите вариант пополнения:</b>")
	for _, plan := range ratePlans {
		planText := fmt.Sprintf("• <b>%s</b> — %.0f ₽\n%s", plan.Title, plan.Amount, plan.Description)
		parts = append(parts, planText)
	}
	message := strings.Join(parts, "\n\n")
	return updateSessionText(bot, chatID, session, stateChooseRate, message, "HTML", rateSelectionKeyboard())
}

func ackCallback(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, text string) {
	cfg := tgbotapi.CallbackConfig{CallbackQueryID: cq.ID}
	if text != "" {
		cfg.Text = text
	}
	bot.Request(cfg)
}

func composeMenuText() string {
	trimmed := strings.TrimSpace(startText)
	if trimmed == "" {
		return "Выберите действие в меню ниже."
	}
	return trimmed + "\n\n<b>Выберите нужный раздел ниже:</b>"
}

func main() {
	pfsenseApiKey := os.Getenv("PFSENSE_API_KEY")
	yookassaApiKey := os.Getenv("YOOKASSA_API_KEY")
	yookassaStoreID := os.Getenv("YOOKASSA_STORE_ID")
	botToken := os.Getenv("TG_BOT_TOKEN")
	tlsKey := os.Getenv("TLS_CRYPT_KEY")
	invoiceToken = os.Getenv("INVOICE_TOKEN")
	invoiceTokenTest = os.Getenv("INVOICE_TOKEN_TEST")
	tlsBytes, _ := os.ReadFile(tlsKey)

	pfsenseClient := pfsense.New(pfsenseApiKey, []byte(tlsBytes))
	yookassaClient = yookassa.New(yookassaStoreID, yookassaApiKey)
	sqliteClient = sqlite.New("database/data.json")
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	go dailyDeductWorker(sqliteClient, bot, pfsenseClient)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.PreCheckoutQuery != nil {
			handlePreCheckout(bot, update.PreCheckoutQuery)
			continue
		}

		if msg := update.Message; msg != nil {
			// handle admin commands safely only when message is present
			if msg.Text == "/revoke" {
				pfsenseClient.RevokeCertificate("68b043fdeeb8d")
				continue
			}
			if msg.Text == "/unrevoke" {
				pfsenseClient.UnrevokeCertificate("68b043fdeeb8d")
				continue
			}

			handleIncomingMessage(bot, msg, pfsenseClient)
			continue
		}

		if cq := update.CallbackQuery; cq != nil && cq.Message != nil {
			handleCallback(bot, cq, pfsenseClient)
		}
	}
}

func revokeAllCertificates(certs []string, pfsenseClient *pfsense.PfSenseClient) {

	var wg sync.WaitGroup
	wg.Add(len(certs))

	errs := make(chan error, len(certs))

	for _, ref := range certs {
		ref := ref // захватываем копию
		go func() {
			defer wg.Done()
			if err := pfsenseClient.RevokeCertificate(ref); err != nil {
				errs <- fmt.Errorf("revoke %s: %w", ref, err)
			}
		}()
	}

	wg.Wait()
	close(errs)

	// можно проверить были ли ошибки (не обязательно)
	for err := range errs {
		log.Println("WARN:", err)
	}

	// // теперь один rebuild
	// if err := pfsenseClient.RebuildCRL(); err != nil {
	// 	log.Println("rebuild error:", err)
	// }
}

func handleIncomingMessage(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, pfsenseClient *pfsense.PfSenseClient) {
	chatID := msg.Chat.ID
	session := getSession(chatID)

	if msg.SuccessfulPayment != nil {
		plan, ok := ratePlanByID[session.PendingPlanID]
		if !ok {
			log.Printf("successful payment received but plan is unknown")
			_ = updateSessionText(bot, chatID, session, stateTopUp, "Не нашли информацию об оплате. Напишите в поддержку.", "", singleBackKeyboard("nav_menu"))
			return
		}
		if err := handleSuccessfulPayment(bot, msg, pfsenseClient, plan, session); err != nil {
			log.Printf("handleSuccessfulPayment error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTopUp, "Не удалось обработать оплату. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
		}
		return
	}

	if msg.IsCommand() {
		switch msg.Command() {
		case "start":
			if err := showMainMenu(bot, chatID, session); err != nil {
				log.Printf("showMainMenu error: %v", err)
			}
		case "pay":
			fakeCallback := &tgbotapi.CallbackQuery{Message: msg, From: msg.From}
			handleGetVPN(bot, fakeCallback, session, pfsenseClient)
		default:
			// ignore other commands
		}
		return
	}
}

func handleCallback(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	session := getSession(chatID)
	data := cq.Data
	ackText := ""

	switch {
	case data == "nav_menu":
		if err := showMainMenu(bot, chatID, session); err != nil {
			log.Printf("showMainMenu error: %v", err)
		}
	case data == "nav_get_vpn":
		handleGetVPN(bot, cq, session, pfsenseClient)
	case data == "nav_topup":
		handleTopUp(bot, cq, session, pfsenseClient)
	case data == "nav_trial" || data == "trial":
		handleTrial(bot, cq, session, pfsenseClient)
	case data == "nav_status":
		handleStatus(bot, cq, session, pfsenseClient)
	case data == "nav_support":
		handleSupport(bot, cq, session)
	case data == "nav_instructions":
		handleInstructionsMenu(bot, cq, session)
	case data == "windows":
		handleInstructionSelection(bot, cq, session, instruct.Windows)
	case data == "android":
		handleInstructionSelection(bot, cq, session, instruct.Android)
	case data == "ios":
		handleInstructionSelection(bot, cq, session, instruct.IOS)
	case strings.HasPrefix(data, "win_prev_"):
		step, _ := strconv.Atoi(strings.TrimPrefix(data, "win_prev_"))
		instruct.InstructionWindows(chatID, bot, step-1)
	case strings.HasPrefix(data, "win_next_"):
		step, _ := strconv.Atoi(strings.TrimPrefix(data, "win_next_"))
		instruct.InstructionWindows(chatID, bot, step+1)
	case strings.HasPrefix(data, "android_prev_"):
		step, _ := strconv.Atoi(strings.TrimPrefix(data, "android_prev_"))
		instruct.InstructionAndroid(chatID, bot, step-1)
	case strings.HasPrefix(data, "android_next_"):
		step, _ := strconv.Atoi(strings.TrimPrefix(data, "android_next_"))
		instruct.InstructionAndroid(chatID, bot, step+1)
	case strings.HasPrefix(data, "ios_prev_"):
		step, _ := strconv.Atoi(strings.TrimPrefix(data, "ios_prev_"))
		instruct.InstructionIos(chatID, bot, step-1)
	case strings.HasPrefix(data, "ios_next_"):
		step, _ := strconv.Atoi(strings.TrimPrefix(data, "ios_next_"))
		instruct.InstructionIos(chatID, bot, step+1)
	case data == "check_payment":
		handleCheckPayment(bot, cq, session, pfsenseClient)
	case strings.HasPrefix(data, "rate_"):
		planID := strings.TrimPrefix(data, "rate_")
		if plan, ok := ratePlanByID[planID]; ok {
			handleRateSelection(bot, cq, session, plan, pfsenseClient)
			return
		}
		ackText = "Неизвестный тариф"
	default:
		// ignore
	}

	ackCallback(bot, cq, ackText)
}

func dailyDeductWorker(store *sqlite.Store, bot *tgbotapi.BotAPI, pfsenseClient *pfsense.PfSenseClient) {
	const (
		checkInterval   = time.Second
		consumptionStep = 10 * time.Second
	)

	ticker := time.NewTicker(checkInterval) // регулярная проверка баланса
	defer ticker.Stop()

	for range ticker.C {
		users := store.GetAllUsers()
		now := time.Now().UTC()

		var certsToRevoke []string

		for userID, userData := range users {
			if userData.Days <= 0 {
				continue
			}

			lastDeduct, err := time.Parse(time.RFC3339, userData.LastDeduct)
			if err != nil {
				log.Printf("invalid lastDeduct for user %s: %v", userID, err)
				continue
			}

			elapsed := now.Sub(lastDeduct)
			if elapsed < consumptionStep {
				continue
			}

			daysToCharge := int64(elapsed / consumptionStep)
			if daysToCharge <= 0 {
				continue
			}

			nextCheckpoint := lastDeduct.Add(time.Duration(daysToCharge) * consumptionStep)
			remaining, err := store.ConsumeDays(userID, daysToCharge, nextCheckpoint)
			if err != nil {
				log.Printf("failed to deduct %d day(s) for user %s: %v", daysToCharge, userID, err)
				continue
			}

			log.Printf("deducted %d day(s) from user %s (remaining: %d)", daysToCharge, userID, remaining)

			if remaining == 0 {
				certRef, err := store.GetCertRef(userID)
				if err != nil {
					log.Printf("failed to find certref of user %s: %v", userID, err)
					continue
				}
				if certRef != "" {
					certsToRevoke = append(certsToRevoke, certRef)
				}

				chatID, err := strconv.ParseInt(userID, 10, 64)
				if err != nil {
					log.Printf("failed to parse chat id %s: %v", userID, err)
					continue
				}
				notifyUserSubscriptionExpired(bot, chatID)
			}
		}

		if len(certsToRevoke) > 0 {
			revokeAllCertificates(certsToRevoke, pfsenseClient)
		}
	}
}

func notifyUserSubscriptionExpired(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "⚠️ Баланс исчерпан! Продлите подписку, чтобы продолжить пользоваться VPN.")
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

func showMainMenu(bot *tgbotapi.BotAPI, chatID int64, session *UserSession) error {
	session.PendingPlanID = ""
	return updateSessionText(bot, chatID, session, stateMenu, composeMenuText(), "HTML", mainMenuInlineKeyboard())
}

func handleGetVPN(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	userID := int64(cq.From.ID)

	if !canProceedKey(userID, "get_vpn", 5*time.Second) {
		ackCallback(bot, cq, "Пожалуйста, немного подождите перед повторным запросом.")
		return
	}

	waitingText := "Готовим для вас конфигурацию VPN..."
	if err := updateSessionText(bot, chatID, session, stateGetVPN, waitingText, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	session.PendingPlanID = ""
	telegramUser := fmt.Sprint(userID)

	certRefID, expiresAt, _, err := ensureUserCertificate(pfsenseClient, telegramUser)
	if err != nil {
		log.Printf("ensureUserCertificate error: %v", err)
		_ = updateSessionText(bot, chatID, session, stateGetVPN, "Не удалось подготовить сертификат. Попробуйте позже или обратитесь в поддержку.", "", singleBackKeyboard("nav_menu"))
		return
	}

	if err := sendCertificate(certRefID, telegramUser, expiresAt, false, chatID, 0, userID, pfsenseClient, bot, session); err != nil {
		log.Printf("sendCertificate error: %v", err)
		_ = updateSessionText(bot, chatID, session, stateGetVPN, "Не удалось отправить файл. Попробуйте позже или обратитесь в поддержку.", "", singleBackKeyboard("nav_menu"))
		return
	}

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d запросил выдачу VPN-конфига", cq.From.ID), cq.From.UserName, bot, userID)
}

func handleTopUp(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	userID := int64(cq.From.ID)

	if !canProceedKey(userID, "top_up", 5*time.Second) {
		ackCallback(bot, cq, "Подождите пару секунд перед новым запросом.")
		return
	}

	telegramUser := fmt.Sprint(userID)
	currentDays, err := sqliteClient.GetDays(telegramUser)
	if err != nil {
		currentDays = 0
	}

	intro := fmt.Sprintf("Текущий баланс: %d дней. Выберите пополнение.", currentDays)
	if err := showRateSelection(bot, chatID, session, intro); err != nil {
		log.Printf("showRateSelection error: %v", err)
		_ = updateSessionText(bot, chatID, session, stateTopUp, "Не удалось показать варианты пополнения. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
		return
	}

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d открыл меню пополнения.", cq.From.ID), cq.From.UserName, bot, userID)
}

func handleTrial(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	userID := int64(cq.From.ID)

	if !canProceedKey(userID, "get_vpn_trial", 5*time.Second) {
		ackCallback(bot, cq, "Пробный VPN можно получать раз в несколько секунд. Попробуйте ещё раз позже.")
		return
	}

	waitingText := "⏳ Создаём пробный сертификат..."
	if err := updateSessionText(bot, chatID, session, stateTrial, waitingText, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	telegramUser := strconv.FormatInt(userID, 10)
	trialDays := 30
	certName := fmt.Sprintf("TrialCert%s_%ddays", telegramUser, trialDays)

	certRefID, certID, err := pfsenseClient.GetCertificateIDByName(certName)
	if err != nil {
		uuid, err := pfsenseClient.GetCARef()
		if err != nil {
			log.Printf("GetCARef error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTrial, "Не удалось выпустить пробный сертификат. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
			return
		}
		certID, certRefID, err = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, trialDays, "", "sha256", telegramUser)
		if err != nil {
			log.Printf("CreateCertificate error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTrial, "Не удалось выпустить пробный сертификат. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
			return
		}
		_, certDateUntil, _, _, err := pfsenseClient.GetDateOfCertificate(certID)
		if err != nil {
			log.Printf("GetDateOfCertificate error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTrial, "Не удалось получить срок действия сертификата. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
			return
		}
		if err := sendCertificate(certRefID, telegramUser, certDateUntil, true, chatID, trialDays, int64(userID), pfsenseClient, bot, session); err != nil {
			log.Printf("sendCertificate error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTrial, "Не удалось отправить сертификат. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
			return
		}
	} else {
		_, certDateUntil, _, expired, err := pfsenseClient.GetDateOfCertificate(certID)
		if err != nil {
			log.Printf("GetDateOfCertificate error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTrial, "Не удалось получить срок действия сертификата. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
			return
		}
		if expired {
			_ = updateSessionText(bot, chatID, session, stateTrial, "Пробный сертификат уже использован. Чтобы продолжить пользоваться VPN, оформите подписку.", "HTML", singleBackKeyboard("nav_menu"))
			return
		}
		if err := sendCertificate(certRefID, telegramUser, certDateUntil, true, chatID, trialDays, userID, pfsenseClient, bot, session); err != nil {
			log.Printf("sendCertificate error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTrial, "Не удалось отправить сертификат. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
			return
		}
	}

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d запросил пробный VPN", cq.From.ID), cq.From.UserName, bot, userID)
}

func handleStatus(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	userID := int64(cq.From.ID)

	if !canProceedKey(userID, "check_status", 3*time.Second) {
		ackCallback(bot, cq, "Подождите пару секунд и попробуйте ещё раз.")
		return
	}

	text, err := buildStatusText(pfsenseClient, int(userID))
	if err != nil {
		log.Printf("buildStatusText error: %v", err)
		text = "Не удалось получить информацию о сертификате. Попробуйте позже."
	}
	finalText := fmt.Sprintf(
		"<b>Профиль:</b>\n"+
			"├ 🪪 ID: <code>%d</code>\n"+
			"└ ✉️ Mail: %s\n"+
			"%s",
		userID, "test", text,
	)
	if err := updateSessionText(bot, chatID, session, stateStatus, finalText, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d проверил статус сертификата", cq.From.ID), cq.From.UserName, bot, userID)
}

func handleSupport(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession) {
	chatID := cq.Message.Chat.ID
	supportText := `📞 <b>Служба поддержки HappyCat VPN</b>

Напиши нам в Telegram: @happycatvpn
<i>Мы отвечаем 24/7 и всегда рядом, если нужна помощь.</i>`

	if err := updateSessionText(bot, chatID, session, stateSupport, supportText, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d открыл раздел поддержки", cq.From.ID), cq.From.UserName, bot, int64(cq.From.ID))
}

func handleInstructionsMenu(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession) {
	chatID := cq.Message.Chat.ID
	instruct.ResetState(chatID)
	text := "Выберите платформу, для которой нужна инструкция:"
	if err := updateSessionText(bot, chatID, session, stateInstructions, text, "", instructionsMenuKeyboard()); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}
}

func handleRateSelection(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, plan RatePlan, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	_ = pfsenseClient

	if err := startPaymentForPlan(bot, chatID, session, plan); err != nil {
		log.Printf("startPaymentForPlan error: %v", err)
		_ = updateSessionText(bot, chatID, session, stateTopUp, "Не удалось сформировать счет. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
		ackCallback(bot, cq, "Не удалось сформировать счет")
		return
	}

	ackCallback(bot, cq, fmt.Sprintf("Счет на <%s> готов", plan.Title))
}

func startPaymentForPlan(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, plan RatePlan) error {
	metadataPlanID := plan.ID
	if metadataPlanID == "" {
		metadataPlanID = strings.ReplaceAll(strings.ToLower(plan.Title), " ", "_")
	}
	metadata := map[string]interface{}{
		"plan_id":     metadataPlanID,
		"plan_title":  plan.Title,
		"plan_days":   plan.Days,
		"plan_amount": plan.Amount,
	}

	newID, replaced, err := yookassaClient.SendVPNPayment(bot, chatID, session.MessageID, plan.Amount, plan.Title, metadata, "")
	if err != nil {
		return err
	}

	if replaced && session.MessageID != 0 && session.MessageID != newID {
		_, _ = bot.Send(tgbotapi.NewDeleteMessage(chatID, session.MessageID))
	}

	session.MessageID = newID
	session.State = stateTopUp
	session.ContentType = "text"
	session.PendingPlanID = metadataPlanID

	instruct.ResetState(chatID)
	return nil
}

func handleInstructionSelection(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, t instruct.InstructType) {
	chatID := cq.Message.Chat.ID
	instruct.SetInstructKeyboard(session.MessageID, chatID, t)

	switch t {
	case instruct.Windows:
		instruct.InstructionWindows(chatID, bot, 0)
	case instruct.Android:
		instruct.InstructionAndroid(chatID, bot, 0)
	case instruct.IOS:
		instruct.InstructionIos(chatID, bot, 0)
	}

	session.State = stateInstructions
	session.ContentType = "photo"
}

func handleCheckPayment(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	paymentID, exists := yookassaClient.IsPaymentExist(chatID)
	if !exists {
		ackCallback(bot, cq, "Активный платеж не найден.")
		return
	}

	payment, err := yookassaClient.GetYooKassaPaymentStatus(paymentID)
	if err != nil {
		log.Printf("GetYooKassaPaymentStatus error: %v", err)
		ackCallback(bot, cq, "Не удалось проверить платеж. Попробуйте позже.")
		return
	}

	if payment.Status != "succeeded" {
		ackCallback(bot, cq, "Платеж еще обрабатывается. Попробуйте чуть позже.")
		return
	}

	yookassaClient.DeletePayment(chatID)

	meta := payment.Metadata
	plan := resolvePlanFromMetadata(meta, session)
	if plan.Title == "" {
		ackCallback(bot, cq, "Не удалось определить выбранный тариф. Напишите в поддержку.")
		return
	}

	fake := &tgbotapi.Message{Chat: cq.Message.Chat, From: cq.From}

	if err := handleSuccessfulPayment(bot, fake, pfsenseClient, plan, session); err != nil {
		log.Printf("handleSuccessfulPayment error: %v", err)
		ackCallback(bot, cq, "Не удалось выдать сертификат. Свяжитесь с поддержкой.")
		return
	}

	ackCallback(bot, cq, fmt.Sprintf("Оплата подтверждена! Тариф «%s» активирован.", plan.Title))
}

func handlePreCheckout(bot *tgbotapi.BotAPI, pcq *tgbotapi.PreCheckoutQuery) {
	ans := tgbotapi.PreCheckoutConfig{
		PreCheckoutQueryID: pcq.ID,
		OK:                 true,
	}
	if _, err := bot.Request(ans); err != nil {
		log.Printf("precheckout answer error: %v", err)
	}
}

func handleSuccessfulPayment(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, pfsenseClient *pfsense.PfSenseClient, plan RatePlan, session *UserSession) error {
	chatID := msg.Chat.ID
	userID := int64(msg.From.ID)
	telegramUser := fmt.Sprint(userID)

	waitingText := fmt.Sprintf("Готовим пополнение «%s». Пожалуйста, подождите...", plan.Title)
	if err := updateSessionText(bot, chatID, session, stateTopUp, waitingText, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	err := issuePlanCertificate(bot, chatID, session, plan, pfsenseClient, telegramUser, userID)
	if err != nil {
		return err
	}

	session.PendingPlanID = ""

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d пополнил баланс пакетом «%s»", msg.From.ID, plan.Title), msg.From.UserName, bot, userID)
	return nil
}

func sendCertificate(certRefID, telegramUserID, certDateUntil string, isProb bool, chatID int64, days int, userID int64, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI, session *UserSession) error {
	var certName string
	if isProb {
		if days <= 0 {
			days = 3
		}
		certName = fmt.Sprintf("TrialCert%s_%ddays", telegramUserID, days)
	} else {
		certName = fmt.Sprintf("Cert%s_permanent", telegramUserID)
	}

	ovpnData, err := pfsenseClient.GenerateOVPN(certRefID, "", "213.21.200.205")
	if err != nil {
		return err
	}

	fileBytes := tgbotapi.FileBytes{
		Name:  certName + ".ovpn",
		Bytes: ovpnData,
	}

	caption := fmt.Sprintf("VPN ID пользователя: <code>%d</code>\nДоступен до: %s", userID, certDateUntil)
	if isProb {
		caption += fmt.Sprintf("\nПробный доступ: %d дней", days)
	} else {
		if days > 0 {
			caption += fmt.Sprintf("\nПополнение: +%d дней", days)
		}
		if balance, err := sqliteClient.GetDays(telegramUserID); err == nil {
			caption += fmt.Sprintf("\nБаланс: %d дней", balance)
		}
	}

	return replaceSessionWithDocument(bot, chatID, session, stateMenu, fileBytes, caption, "HTML", singleBackKeyboard("nav_menu"))
}

func buildStatusText(pfsenseClient *pfsense.PfSenseClient, userID int) (string, error) {
	telegramUser := fmt.Sprint(userID)
	_, _, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUser)
	days, _ := sqliteClient.GetDays(strconv.Itoa(userID))

	if err != nil {
		return fmt.Sprintf(`<b>Статус подписки:</b>
<b>├ 🔴 Неактивна:</b>
<b>└ ⏳ Дней на балансе:</b> %d
Пополните баланс, чтобы пользоваться VPN.`, days), nil
	}

	// certID, certName, err := pfsenseClient.GetCertificateIDByRefid(certRefID)
	// if err != nil {
	// 	return "", err
	// }

	// _, until, daysLeft, expired, err := pfsenseClient.GetDateOfCertificate(certID)
	// if err != nil {
	// 	return "", err
	// }

	// planDays := extractDaysFromCertName(certName)

	if days == 0 {
		return fmt.Sprintf(`<b>Статус подписки:</b>
<b>├ 🔴 Неактивна</b>
<b>└ ⏳ Дней на балансе:</b> %d
Пополните баланс, чтобы пользоваться VPN.`, days), nil
	}

	return fmt.Sprintf(`<b>Статус подписки:</b>
<b>├ 🟢 Активна</b>
<b>└ ⏳ Дней на балансе:</b> %d
────────────────────────
✅ Отличная новость — VPN работает!`, days), nil
}

func sendMessageToAdmin(text string, username string, bot *tgbotapi.BotAPI, id int64) {
	if id == 623290294 {
		return
	}
	var userLink string
	if username != "" {
		userLink = fmt.Sprintf("<a href=\"https://t.me/%s\">@%s</a>", html.EscapeString(username), html.EscapeString(username))
	} else {
		userLink = fmt.Sprintf("<a href=\"tg://user?id=%d\">Профиль пользователя</a>", id)
	}
	newText := fmt.Sprintf("%s:\n%s", userLink, html.EscapeString(text))
	msg := tgbotapi.NewMessage(623290294, newText)
	msg.ParseMode = "HTML"

	msg2 := tgbotapi.NewMessage(6365653009, newText)
	msg2.ParseMode = "HTML"
	bot.Send(msg)
	bot.Send(msg2)

}
