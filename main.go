package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	instruct "github.com/Asort97/vpnBot/clients/instruction"
	pfsense "github.com/Asort97/vpnBot/clients/pfSense"
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

type SessionState string

const (
	stateMenu         SessionState = "menu"
	stateGetVPN       SessionState = "get_vpn"
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
	Description string
}

// ratePlans содержит список доступных тарифов. При необходимости поменяйте названия и цены.
var ratePlans = []RatePlan{
	{ID: "7d", Title: "7 дней", Amount: 199, Description: "Подходит, чтобы попробовать сервис или уехать на короткий срок."},
	{ID: "30d", Title: "30 дней", Amount: 699, Description: "Оптимальный вариант для постоянного доступа без ограничений."},
	{ID: "180d", Title: "6 месяцев", Amount: 3499, Description: "Экономия по сравнению с помесячной оплатой и минимум хлопот."},
	{ID: "365d", Title: "12 месяцев", Amount: 5999, Description: "Максимальная выгода для тех, кто всегда онлайн."},
}

var ratePlanByID = func() map[string]RatePlan {
	result := make(map[string]RatePlan)
	for _, plan := range ratePlans {
		result[plan.ID] = plan
	}
	return result
}()

type UserSession struct {
	MessageID   int
	State       SessionState
	ContentType string
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
			tgbotapi.NewInlineKeyboardButtonData("🐱 Получить VPN", "nav_get_vpn"),
			tgbotapi.NewInlineKeyboardButtonData("Пробный VPN 🎁", "nav_trial"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🙍 Профиль", "nav_status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📚 Инструкции", "nav_instructions"),
			tgbotapi.NewInlineKeyboardButtonData("Поддержка 🆘 ", "nav_support"),
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
	for _, plan := range ratePlans {
		label := fmt.Sprintf("%s — %.0f ₽", plan.Title, plan.Amount)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "rate_"+plan.ID),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", "nav_menu"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func showRateSelection(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, intro string) error {
	var parts []string
	if strings.TrimSpace(intro) != "" {
		parts = append(parts, intro)
	}
	parts = append(parts, "<b>Доступные тарифы:</b>")
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

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.PreCheckoutQuery != nil {
			handlePreCheckout(bot, update.PreCheckoutQuery)
			continue
		}

		if msg := update.Message; msg != nil {
			handleIncomingMessage(bot, msg, pfsenseClient)
			continue
		}

		if cq := update.CallbackQuery; cq != nil && cq.Message != nil {
			handleCallback(bot, cq, pfsenseClient)
		}
	}
}

func handleIncomingMessage(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, pfsenseClient *pfsense.PfSenseClient) {
	chatID := msg.Chat.ID
	session := getSession(chatID)

	if msg.SuccessfulPayment != nil {
		handleSuccessfulPayment(bot, msg, pfsenseClient, session)
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
			handleRateSelection(bot, cq, session, plan)
			ackText = fmt.Sprintf("Тариф «%s» выбран", plan.Title)
		} else {
			ackText = "Неизвестный тариф"
		}
	default:
		// ignore
	}

	ackCallback(bot, cq, ackText)
}

func showMainMenu(bot *tgbotapi.BotAPI, chatID int64, session *UserSession) error {
	return updateSessionText(bot, chatID, session, stateMenu, composeMenuText(), "HTML", mainMenuInlineKeyboard())
}

func handleGetVPN(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, pfsenseClient *pfsense.PfSenseClient) {
	chatID := cq.Message.Chat.ID
	userID := int64(cq.From.ID)

	if !canProceedKey(userID, "get_vpn", 5*time.Second) {
		ackCallback(bot, cq, "Подождите пару секунд и попробуйте снова.")
		return
	}

	waitingText := "⏳ Готовим ваш VPN, это займёт пару секунд..."
	if err := updateSessionText(bot, chatID, session, stateGetVPN, waitingText, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	telegramUser := fmt.Sprint(userID)
	_, isExist := pfsenseClient.IsUserExist(telegramUser)

	if isExist {
		if err := processExistingUser(bot, chatID, session, pfsenseClient, telegramUser, userID); err != nil {
			log.Printf("processExistingUser error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateGetVPN, "Не получилось проверить подписку. Попробуйте ещё раз позже.", "", singleBackKeyboard("nav_menu"))
		}
	} else {
		intro := "Сертификат для вашего аккаунта не найден. Выберите тариф, чтобы оформить подписку."
		if err := showRateSelection(bot, chatID, session, intro); err != nil {
			log.Printf("showRateSelection error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateGetVPN, "Не удалось показать тарифы. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
		}
	}

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d открыл покупку VPN", cq.From.ID), cq.From.UserName, bot, userID)
}

func processExistingUser(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, pfsenseClient *pfsense.PfSenseClient, telegramUser string, userID int64) error {
	_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUser)
	if err != nil {
		return createUserAndSendCertificate(chatID, int(userID), pfsenseClient, bot, session)
	}

	certID, err := pfsenseClient.GetCertificateIDByRefid(certRefID)
	if err != nil {
		return createUserAndSendCertificate(chatID, int(userID), pfsenseClient, bot, session)
	}

	_, certDateUntil, _, expired, err := pfsenseClient.GetDateOfCertificate(certID)
	if err != nil {
		return err
	}

	if expired {
		intro := "Срок действия подписки закончился. Выберите тариф, чтобы продлить доступ."
		return showRateSelection(bot, chatID, session, intro)
	}

	return sendCertificate(certRefID, telegramUser, certDateUntil, false, chatID, userID, pfsenseClient, bot, session)
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
	certName := fmt.Sprintf("TrialCert%s", telegramUser)

	certRefID, certID, err := pfsenseClient.GetCertificateIDByName(certName)
	if err != nil {
		uuid, err := pfsenseClient.GetCARef()
		if err != nil {
			log.Printf("GetCARef error: %v", err)
			_ = updateSessionText(bot, chatID, session, stateTrial, "Не удалось выпустить пробный сертификат. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
			return
		}
		certID, certRefID, err = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, 3, "", "sha256", telegramUser)
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
		if err := sendCertificate(certRefID, telegramUser, certDateUntil, true, chatID, userID, pfsenseClient, bot, session); err != nil {
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
		if err := sendCertificate(certRefID, telegramUser, certDateUntil, true, chatID, userID, pfsenseClient, bot, session); err != nil {
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
			"   🪪 ID: <code>%d</code>\n"+
			"   ✉️ Mail: %s\n"+
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

func handleRateSelection(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, session *UserSession, plan RatePlan) {
	chatID := cq.Message.Chat.ID

	waiting := fmt.Sprintf("⏳ Подготавливаем оплату тарифа <b>%s</b>...", plan.Title)
	if err := updateSessionText(bot, chatID, session, stateGetVPN, waiting, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	if err := startPaymentForPlan(bot, chatID, session, plan); err != nil {
		log.Printf("startPaymentForPlan error: %v", err)
		_ = updateSessionText(bot, chatID, session, stateGetVPN, "Не удалось подготовить оплату. Попробуйте позже.", "", singleBackKeyboard("nav_menu"))
	}
}

func startPaymentForPlan(bot *tgbotapi.BotAPI, chatID int64, session *UserSession, plan RatePlan) error {
	oldID := session.MessageID
	newID, replaced, err := yookassaClient.SendVPNPayment(bot, chatID, session.MessageID, plan.Amount, plan.Title, "")
	if err != nil {
		return err
	}
	if replaced && oldID != 0 && oldID != newID {
		_, _ = bot.Send(tgbotapi.NewDeleteMessage(chatID, oldID))
	}
	session.MessageID = newID
	session.State = stateGetVPN
	session.ContentType = "text"
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
		ackCallback(bot, cq, "Активный платёж не найден.")
		return
	}

	fmt.Printf("GET STATUS PAYMENT %s", paymentID)

	payment, err := yookassaClient.GetYooKassaPaymentStatus(paymentID)

	fmt.Printf("GET PAYMENT ID %s", payment.Metadata["product"].(string))
	product := payment.Metadata["product"].(string)

	if err != nil {
		log.Printf("GetYooKassaPaymentStatus error: %v", err)
		ackCallback(bot, cq, "Не удалось проверить платёж. Попробуйте позже.")
		return
	}

	if payment.Status == "succeeded" {
		yookassaClient.DeletePayment(chatID)
		fake := &tgbotapi.Message{Chat: cq.Message.Chat, From: cq.From}

		switch product {
		case "7 дней":
			handleSuccessfulPayment(bot, fake, pfsenseClient, 7, session)
		case "30 дней":
			handleSuccessfulPayment(bot, fake, pfsenseClient, 30, session)
		case "6 месяцев":
			handleSuccessfulPayment(bot, fake, pfsenseClient, 186, session)
		case "12 месяцев":
			handleSuccessfulPayment(bot, fake, pfsenseClient, 365, session)
		}
		ackCallback(bot, cq, "Оплата подтверждена! Генерируем сертификат.")
		return
	}

	ackCallback(bot, cq, "Платёж ещё обрабатывается. Попробуйте снова через минуту.")
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

func handleSuccessfulPayment(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, pfsenseClient *pfsense.PfSenseClient, days int, session *UserSession) {
	chatID := msg.Chat.ID
	userID := int64(msg.From.ID)

	waitingText := "✅ Оплата получена. Генерируем сертификат..."
	if err := updateSessionText(bot, chatID, session, stateGetVPN, waitingText, "HTML", singleBackKeyboard("nav_menu")); err != nil {
		log.Printf("updateSessionText error: %v", err)
	}

	if err := createUserAndSendCertificate(chatID, int(userID), pfsenseClient, bot, days, session); err != nil {
		log.Printf("createUserAndSendCertificate error: %v", err)
		_ = updateSessionText(bot, chatID, session, stateGetVPN, "Не удалось выдать сертификат. Напишите в поддержку.", "", singleBackKeyboard("nav_menu"))
		return
	}

	sendMessageToAdmin(fmt.Sprintf("Пользователь id:%d оплатил VPN", msg.From.ID), msg.From.UserName, bot, userID)
}

func createUserAndSendCertificate(chatID int64, userID int, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI, days int, session *UserSession) error {
	telegramUser := fmt.Sprint(userID)
	certName := fmt.Sprintf("Cert%s", telegramUser)

	userIDStr, exists := pfsenseClient.IsUserExist(telegramUser)
	if !exists {
		var err error
		userIDStr, err = pfsenseClient.CreateUser(telegramUser, "123", "", "", false)
		if err != nil {
			return err
		}
	}

	_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUser)
	if err != nil {
		uuid, err := pfsenseClient.GetCARef()
		if err != nil {
			return err
		}
		certID, certRefID, err := pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, days, "", "sha256", telegramUser)
		if err != nil {
			return err
		}
		if err := pfsenseClient.AttachCertificateToUser(userIDStr, certRefID); err != nil {
			return err
		}
		_, certDateUntil, _, _, err := pfsenseClient.GetDateOfCertificate(certID)
		if err != nil {
			return err
		}
		return sendCertificate(certRefID, telegramUser, certDateUntil, false, chatID, int64(userID), pfsenseClient, bot, session)
	}

	certID, err := pfsenseClient.GetCertificateIDByRefid(certRefID)
	if err != nil {
		return err
	}

	_, certDateUntil, _, _, err := pfsenseClient.GetDateOfCertificate(certID)
	if err != nil {
		return err
	}

	return sendCertificate(certRefID, telegramUser, certDateUntil, false, chatID, int64(userID), pfsenseClient, bot, session)
}

func sendCertificate(certRefID, telegramUserID, certDateUntil string, isProb bool, chatID int64, userID int64, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI, session *UserSession) error {
	var certName string
	if isProb {
		certName = fmt.Sprintf("TrialCert%s", telegramUserID)
	} else {
		certName = fmt.Sprintf("Cert%s", telegramUserID)
	}

	ovpnData, err := pfsenseClient.GenerateOVPN(certRefID, "", "213.21.200.205")
	if err != nil {
		return err
	}

	fileBytes := tgbotapi.FileBytes{
		Name:  certName + ".ovpn",
		Bytes: ovpnData,
	}

	caption := fmt.Sprintf("🐾 ID пользователя: %d\n📅 Сертификат активен до: %s", userID, certDateUntil)

	return replaceSessionWithDocument(bot, chatID, session, stateMenu, fileBytes, caption, "HTML", singleBackKeyboard("nav_menu"))
}

func buildStatusText(pfsenseClient *pfsense.PfSenseClient, userID int) (string, error) {
	telegramUser := fmt.Sprint(userID)
	_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUser)
	if err != nil {
		return (`<b>Статус подписки:</b>
	<b>🔴 Неактивен с:</b>`), nil
	}

	certID, err := pfsenseClient.GetCertificateIDByRefid(certRefID)
	if err != nil {
		return "", err
	}

	from, until, daysLeft, expired, err := pfsenseClient.GetDateOfCertificate(certID)
	if err != nil {
		return "", err
	}

	if expired {
		return (`<b>Статус подписки:</b>
	<b>🔴 Неактивен с:</b>`), nil
	}

	return fmt.Sprintf(`<b>Статус подписки:</b>
	<b>🟢 Активен с:</b> %s
	<b>📆 Действует до:</b> %s
	<b>⏳ Осталось дней:</b> %d
────────────────────────
✅ Отличная новость — VPN работает!`, from, until, daysLeft), nil
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
	bot.Send(msg)
}
