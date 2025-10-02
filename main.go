package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	colorfulprint "github.com/Asort97/vpnBot/clients/colorfulPrint"
	pfsense "github.com/Asort97/vpnBot/clients/pfSense"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type InstructionState struct {
	CurrentStep int
	MessageID   int
	ChatID      int64
}

var instructionMessage tgbotapi.Message

// var instructionStates = make(map[int64]*InstructionState)
var (
	windowsStates = make(map[int64]*InstructionState)
	androidStates = make(map[int64]*InstructionState)
	iosStates     = make(map[int64]*InstructionState)
)

const startText = `
👋 <b>Добро пожаловать в HappyCat VPN!</b>

🔒 <u>С нашим сервисом вы получите:</u>
• Быстрый и стабильный доступ к интернету без ограничений
• Надёжное шифрование и защиту ваших данных
• Подключение в пару кликов

🎁 Новым пользователям доступен <b>бесплатный пробный период на 3 дня</b> — попробуйте VPN и оцените качество без рисков!

➡️ Нажмите <b>«🆓 Пробный доступ»</b>, чтобы подключить пробный период.
Для подключения используйте инструкцию!
`

var lastActionKey = make(map[int64]map[string]time.Time)

const vpnCost int = 180

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

func main() {
	pfsenseApiKey := os.Getenv("PFSENSE_API_KEY")
	botToken := os.Getenv("TG_BOT_TOKEN")
	tlsKey := os.Getenv("TLS_CRYPT_KEY")
	tlsBytes, _ := os.ReadFile(tlsKey)
	pfsenseClient := pfsense.New(pfsenseApiKey, []byte(tlsBytes))
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {

		if update.PreCheckoutQuery != nil {
			handlePreCheckout(bot, update.PreCheckoutQuery)
			continue
		}

		if update.Message != nil {

			if update.Message.SuccessfulPayment != nil {
				handleSuccessfulPayment(bot, update.Message, pfsenseClient)
				continue
			}

			if update.Message.IsCommand() && update.Message.Command() == "start" {
				sendStart(bot, update.Message.Chat.ID)
				// time.Sleep(1 * time.Second)

				// msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Меню под клавиатурой:")
				// msg.ReplyMarkup = menuKeyboard()
				// bot.Send(msg)
				continue
			}

			switch update.Message.Text {
			case "🔑 Получить VPN":
				if !canProceedKey(update.Message.From.ID, "get_vpn", 5*time.Second) {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "⏳ Подождите ~5 сек перед повторной выдачей VPN"))
					break
				}

				msgWait := tgbotapi.NewMessage(update.Message.Chat.ID, "Пожалуйста подождите...")
				bot.Send(msgWait)

				telegramUserid := fmt.Sprint(update.Message.From.ID)
				_, isExist := pfsenseClient.IsUserExist(telegramUserid)

				if isExist {
					_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUserid)

					if err != nil {
						createUserAndSendCertificate(update, pfsenseClient, bot)
					} else {
						certID, _ := pfsenseClient.GetCertificateIDByRefid(certRefID)
						_, _, _, expired, _ := pfsenseClient.GetDateOfCertificate(certID)
						// expired := true
						if expired {
							msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Ваша подписка истекла! Чтобы получить VPN пожалуйста обновите подписку!")
							bot.Send(msg)

							_ = sendStarsInvoice(bot, update.Message.Chat.ID, vpnCost)
						} else {
							_, certDateUntil, _, _, _ := pfsenseClient.GetDateOfCertificate(certID)

							sendCertificate(certRefID, telegramUserid, certDateUntil, false, update, pfsenseClient, bot)
							// createUserAndSendCertificate(update, pfsenseClient, bot)
						}
					}
				} else {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Здравствуйте! Чтобы получить VPN оплатите подписку!")
					bot.Send(msg)

					// amount := 250
					_ = sendStarsInvoice(bot, update.Message.Chat.ID, vpnCost)
				}

				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Получить VPN...", update.Message.From.ID), update.Message.From.UserName, bot)

				continue

			case "📖 Инструкция":
				buttons := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🪟 Windows", "windows"),
						tgbotapi.NewInlineKeyboardButtonData("📱 Android", "android"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🍎 IOS", "ios"),
					),
				)

				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Выберите действие:")
				msg.ReplyMarkup = buttons
				instructionMessage, _ = bot.Send(msg)

				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Инструкции...", update.Message.From.ID), update.Message.From.UserName, bot)
				// instructionWindows(update, bot)
				continue

			case "📊 Проверить статус":
				if !canProceedKey(update.Message.From.ID, "check_status", 3*time.Second) {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "⏳ Чуть позже, подождите пару секунд"))
					break
				}
				checkStatus(pfsenseClient, update, bot)

				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Проверки статуса...", update.Message.From.ID), update.Message.From.UserName, bot)

				continue

			case "💬 Поддержка":
				supportText := `🛠️ <b>Техническая поддержка</b>

Если у вас возникли проблемы:
• 🔧 Телеграм: https://t.me/happycatvpn

⏰ <i>Время ответа: до 24 часов</i>`

				msg := tgbotapi.NewMessage(update.Message.Chat.ID, supportText)
				msg.ParseMode = "HTML"
				bot.Send(msg)

				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Поддержки...", update.Message.From.ID), update.Message.From.UserName, bot)

				continue

			case "🆓 Пробный доступ":

				msgWait := tgbotapi.NewMessage(update.Message.Chat.ID, "Пожалуйста подождите...")
				bot.Send(msgWait)

				if !canProceedKey(update.Message.From.ID, "get_vpn_trial", 5*time.Second) {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "⏳ Подождите ~5 сек перед повторной выдачей VPN"))
					break
				}

				createProbCertificate(update, pfsenseClient, bot)
				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Пробного доступа...", update.Message.From.ID), update.Message.From.UserName, bot)

				continue

			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Воспользуйтесь меню под клавиатурой!:")
			msg.ReplyMarkup = menuKeyboard()
			bot.Send(msg)
		}

		if cq := update.CallbackQuery; cq != nil && cq.Message != nil {

			chatID := cq.Message.Chat.ID

			if strings.HasPrefix(cq.Data, "win_prev_") {
				// Обработка кнопки "Назад"
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "win_prev_"))
				newStep := currentStep - 1
				instructionWindows(chatID, bot, newStep)

			} else if strings.HasPrefix(cq.Data, "win_next_") {
				// Обработка кнопки "Вперед"
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "win_next_"))
				newStep := currentStep + 1
				instructionWindows(chatID, bot, newStep)
			}

			if strings.HasPrefix(cq.Data, "android_prev_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "android_prev_"))
				instructionAndroid(chatID, bot, currentStep-1)
			} else if strings.HasPrefix(cq.Data, "android_next_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "android_next_"))
				instructionAndroid(chatID, bot, currentStep+1)
			}

			// Обработка iOS
			if strings.HasPrefix(cq.Data, "ios_prev_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "ios_prev_"))
				instructionIos(chatID, bot, currentStep-1)
			} else if strings.HasPrefix(cq.Data, "ios_next_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "ios_next_"))
				instructionIos(chatID, bot, currentStep+1)
			}

			switch cq.Data {
			case "windows":
				instructionWindows(chatID, bot, 0)
			case "android":
				instructionAndroid(chatID, bot, 0)
			case "ios":
				instructionIos(chatID, bot, 0)
			// case "trial":
			// 	createProbCertificate(update, pfsenseClient, bot)
			case "trial":
				createProbCertificate(update, pfsenseClient, bot)
			}
			bot.Request(tgbotapi.NewCallback(cq.ID, ""))
		}
	}
}

func sendMessageToAdmin(text string, username string, bot *tgbotapi.BotAPI) {

	newText := fmt.Sprintf("@%s:\n%s", username, text)
	msg := tgbotapi.NewMessage(623290294, newText)
	bot.Send(msg)
}

func checkStatus(pfsenseClient *pfsense.PfSenseClient, update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(fmt.Sprint(update.Message.From.ID))
	if err != nil {
		colorfulprint.PrintError("Unable to found user %e\n", err)
	}
	certId, err := pfsenseClient.GetCertificateIDByRefid(certRefID)
	if err != nil {
		colorfulprint.PrintError(fmt.Sprintf("Unable to found certificate id:%s by refid:%s %e\n", certId, certRefID, err), err)
	} else {
		colorfulprint.PrintState(fmt.Sprintf("Founded attached cert id:%s of USER:%d and OUR ID OF CERT:%s\n", certRefID, update.Message.From.ID, certId))
	}

	from, until, daysLeft, expired, err := pfsenseClient.GetDateOfCertificate(certId)
	if err != nil {
		colorfulprint.PrintError(fmt.Sprintf("Couldnt get date of certificate{%s}\n", certId), err)
	}

	var statusIcon, statusText string
	if expired {
		statusIcon = "❌"
		statusText = "Истекла"
	} else {
		statusIcon = "✅"
		statusText = "Активна"
	}

	text := fmt.Sprintf(`📊 <b>Статус вашей подписки</b>

%s <b>Статус:</b> %s
📅 <b>Начало:</b> %s
⏰ <b>Окончание:</b> %s
—————————————————
💡 Осталось: %d дней`,
		statusIcon, statusText, from, until, daysLeft)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
	msg.ParseMode = "HTML"
	bot.Send(msg)

}

func instructionWindows(chatID int64, bot *tgbotapi.BotAPI, step int) {
	steps := []struct {
		photoPath string
		caption   string
	}{
		{
			"InstructionPhotos/Windows/1.png",
			"1) Скачайте <a href=\"https://openvpn.net/community/\">OpenVPN</a> с официального сайта",
		},
		{
			"InstructionPhotos/Windows/2.png",
			"2) После скачивания откройте трей в правом нижнем углу",
		},
		{
			"InstructionPhotos/Windows/3.png",
			"3) Нажмите правой кнопкой мыши по значку OpenVPN и далее Импорт->Импорт файла конфигурации и выберите файл конфигурации который мы вам отправим",
		},
		{
			"InstructionPhotos/Windows/4.png",
			"4) Далее нажмите правой кнопкой по значку снова и нажмите кнопку Подключиться",
		},
	}

	// Проверяем границы шагов
	if step < 0 {
		step = 0
	}
	if step >= len(steps) {
		step = len(steps) - 1
	}

	// Удаляем предыдущее сообщение если есть
	if state, exists := windowsStates[chatID]; exists && state.MessageID != 0 {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.MessageID)
		bot.Send(deleteMsg)
	}

	// Создаем кнопки навигации
	var row []tgbotapi.InlineKeyboardButton

	if step > 0 {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("win_prev_%d", step)))
	}

	row = append(row, tgbotapi.NewInlineKeyboardButtonData(
		fmt.Sprintf("Шаг %d/%d", step+1, len(steps)), "win_current"))

	if step < len(steps)-1 {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("win_next_%d", step)))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)

	// Отправляем новое сообщение с фото
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(steps[step].photoPath))
	photo.Caption = steps[step].caption
	photo.ParseMode = "HTML"
	photo.ReplyMarkup = keyboard

	msg, err := bot.Send(photo)
	if err != nil {
		log.Printf("Error sending photo: %v", err)
		return
	}

	// Сохраняем состояние
	windowsStates[chatID] = &InstructionState{
		CurrentStep: step,
		MessageID:   msg.MessageID,
		ChatID:      chatID,
	}
}

func instructionAndroid(chatID int64, bot *tgbotapi.BotAPI, step int) {
	steps := []struct {
		photoPath string
		caption   string
	}{
		{
			"",
			"1) Скачайте <a href=\"https://play.google.com/store/apps/details?id=net.openvpn.openvpn\">OpenVPN</a> с GooglePlay",
		},
		{
			"InstructionPhotos/Android/1.jpg",
			"2) Откройте файловый менеджер и найдите там файл сертификата",
		},
		{
			"InstructionPhotos/Android/2.jpg",
			"3) Нажмите на файл и выберите в меню OpenVPN",
		},
		{
			"InstructionPhotos/Android/3.jpg",
			"4) Нажмите OK и подключитесь",
		},
	}

	// Проверяем границы шагов
	if step < 0 {
		step = 0
	}
	if step >= len(steps) {
		step = len(steps) - 1
	}

	// Удаляем предыдущее сообщение если есть
	if state, exists := androidStates[chatID]; exists && state.MessageID != 0 {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.MessageID)
		bot.Send(deleteMsg)
	}

	// Создаем кнопки навигации
	var row []tgbotapi.InlineKeyboardButton

	if step > 0 {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("android_prev_%d", step)))
	}

	row = append(row, tgbotapi.NewInlineKeyboardButtonData(
		fmt.Sprintf("Android %d/%d", step+1, len(steps)), "android_current"))

	if step < len(steps)-1 {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("android_next_%d", step)))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)

	// Для первого шага (без фото) отправляем текстовое сообщение
	if step == 0 {
		msg := tgbotapi.NewMessage(chatID, steps[step].caption)
		msg.ParseMode = "HTML"
		msg.ReplyMarkup = keyboard

		sentMsg, err := bot.Send(msg)
		if err == nil {
			androidStates[chatID] = &InstructionState{
				CurrentStep: step,
				MessageID:   sentMsg.MessageID,
				ChatID:      chatID,
			}
		}
		return
	}

	// Для остальных шагов отправляем фото
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(steps[step].photoPath))
	photo.Caption = steps[step].caption
	photo.ParseMode = "HTML"
	photo.ReplyMarkup = keyboard

	msg, err := bot.Send(photo)
	if err != nil {
		log.Printf("Error sending Android photo: %v", err)
		return
	}

	// Сохраняем состояние
	androidStates[chatID] = &InstructionState{
		CurrentStep: step,
		MessageID:   msg.MessageID,
		ChatID:      chatID,
	}
}
func instructionIos(chatID int64, bot *tgbotapi.BotAPI, step int) {
	steps := []struct {
		photoPath string
		caption   string
	}{
		{
			"",
			"1) Скачайте <a href=\"https://apps.apple.com/au/app/openvpn-connect/id590379981\">OpenVPN</a> с AppStore",
		},
		{
			"InstructionPhotos/Ios/1.jpg",
			"2) Откройте файловый менеджер на вашем устройстве",
		},
		{
			"InstructionPhotos/Ios/2.jpg",
			"3) Найдите файл сертификата",
		},
		{
			"InstructionPhotos/Ios/4.png",
			"4) Откройте через OpenVPN",
		},
		{
			"InstructionPhotos/Ios/5.jpg",
			"5) Нажмите кнопку ADD и подключайтесь!",
		},
	}

	// Проверяем границы шагов
	if step < 0 {
		step = 0
	}
	if step >= len(steps) {
		step = len(steps) - 1
	}

	// Удаляем предыдущее сообщение если есть
	if state, exists := iosStates[chatID]; exists && state.MessageID != 0 {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.MessageID)
		bot.Send(deleteMsg)
	}

	// Создаем кнопки навигации
	var row []tgbotapi.InlineKeyboardButton

	if step > 0 {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("ios_prev_%d", step)))
	}

	row = append(row, tgbotapi.NewInlineKeyboardButtonData(
		fmt.Sprintf("iOS %d/%d", step+1, len(steps)), "ios_current"))

	if step < len(steps)-1 {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("ios_next_%d", step)))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)

	// Для первого шага (без фото) отправляем текстовое сообщение
	if step == 0 {
		msg := tgbotapi.NewMessage(chatID, steps[step].caption)
		msg.ParseMode = "HTML"
		msg.ReplyMarkup = keyboard

		sentMsg, err := bot.Send(msg)
		if err == nil {
			iosStates[chatID] = &InstructionState{
				CurrentStep: step,
				MessageID:   sentMsg.MessageID,
				ChatID:      chatID,
			}
		}
		return
	}

	// Для остальных шагов отправляем фото
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(steps[step].photoPath))
	photo.Caption = steps[step].caption
	photo.ParseMode = "HTML"
	photo.ReplyMarkup = keyboard

	msg, err := bot.Send(photo)
	if err != nil {
		log.Printf("Error sending iOS photo: %v", err)
		return
	}

	// Сохраняем состояние
	iosStates[chatID] = &InstructionState{
		CurrentStep: step,
		MessageID:   msg.MessageID,
		ChatID:      chatID,
	}
}

func menuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔑 Получить VPN"),
			tgbotapi.NewKeyboardButton("🆓 Пробный доступ"),
			tgbotapi.NewKeyboardButton("📊 Проверить статус"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📖 Инструкция"),
			tgbotapi.NewKeyboardButton("💬 Поддержка"),
		),
	)
	kb.ResizeKeyboard = true
	kb.OneTimeKeyboard = false
	return kb
}

func createUserAndSendCertificate(update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI) {

	telegramUserid := fmt.Sprint(update.Message.From.ID)
	certName := fmt.Sprintf("Cert%s", telegramUserid)

	var userID string
	userID, isExist := pfsenseClient.IsUserExist(telegramUserid)

	if isExist {
		fmt.Printf("User{%s} is exist and there is his id:%s!\n", telegramUserid, userID)
	} else {
		fmt.Printf("User{%s} is not exist!\n", telegramUserid)
		userID, _ = pfsenseClient.CreateUser(telegramUserid, "123", "", "", false)
	}

	var certRefID string
	var certID string

	_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUserid)

	if err != nil {
		colorfulprint.PrintError(fmt.Sprintf("Couldnt find attached certificate on user{%s}\n", telegramUserid), err)

		uuid, _ := pfsenseClient.GetCARef()
		certID, certRefID, _ = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, 30, "", "sha256", telegramUserid)
		pfsenseClient.AttachCertificateToUser(userID, certRefID)
	} else {
		certID, _ = pfsenseClient.GetCertificateIDByRefid(certRefID)
		_, _, _, expired, _ := pfsenseClient.GetDateOfCertificate(certID)
		// expired := true
		if expired {
			// Логика удаления !!!!!!!!!
			pfsenseClient.DeleteUserCertificate(certID)
			//После удаления создаем новый сертификат и привязываем его к пользователю
			uuid, _ := pfsenseClient.GetCARef()
			certID, certRefID, _ = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, 30, "", "sha256", telegramUserid)
			pfsenseClient.AttachCertificateToUser(userID, certRefID)

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Создан новый сертификат!")
			bot.Send(msg)
		}
	}

	_, certDateUntil, _, _, _ := pfsenseClient.GetDateOfCertificate(certID)

	ovpnData, err := pfsenseClient.GenerateOVPN(certRefID, "", "213.21.200.205")
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Error generating OVPN: "+err.Error())
		bot.Send(msg)
	}

	// Отправка OVPN в Telegram как файл
	fileBytes := tgbotapi.FileBytes{
		Name:  certName + ".ovpn",
		Bytes: ovpnData,
	}
	docMsg := tgbotapi.NewDocument(update.Message.Chat.ID, fileBytes)
	docMsg.ReplyToMessageID = update.Message.MessageID
	bot.Send(docMsg)

	// Подтверждение в чате
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Создан новый пользователь с userID:{%d} и отправлен VPN\nИстекает: %s", update.Message.From.ID, certDateUntil))
	msg.ReplyToMessageID = update.Message.MessageID
	bot.Send(msg)
}

func createProbCertificate(update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI) {
	var chatID int64
	var userID int64

	if update.Message != nil {
		chatID = update.Message.Chat.ID
		userID = int64(update.Message.From.ID)
	} else if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		chatID = update.CallbackQuery.Message.Chat.ID
		userID = int64(update.CallbackQuery.From.ID)
	} else {
		return
	}

	telegramUserid := strconv.FormatInt(userID, 10) // ок для int64

	certName := fmt.Sprintf("TrialCert%s", telegramUserid)

	var certRefID, certID string
	var err error

	certRefID, certID, err = pfsenseClient.GetCertificateIDByName(certName)
	if err != nil {
		uuid, _ := pfsenseClient.GetCARef()
		certID, certRefID, _ = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, 3, "", "sha256", telegramUserid)
		_, certDateUntil, _, _, _ := pfsenseClient.GetDateOfCertificate(certID)

		sendCertificate(certRefID, telegramUserid, certDateUntil, true, update, pfsenseClient, bot)
		return
	}

	_, certDateUntil, _, expired, _ := pfsenseClient.GetDateOfCertificate(certID)
	if expired {
		msg := tgbotapi.NewMessage(chatID, "Ваш пробный доступ к VPN закончился. Предлагаем продолжить пользоваться нашими услугами и оплатить подписку!")
		bot.Send(msg)
		return
	}

	sendCertificate(certRefID, telegramUserid, certDateUntil, true, update, pfsenseClient, bot)
}

func sendStarsInvoice(bot *tgbotapi.BotAPI, chatID int64, amountStars int) error {
	// if amountStars <= 0 {
	// 	amountStars = 1
	// }
	prices := []tgbotapi.LabeledPrice{
		{Label: "VPN Premium на 30 дней", Amount: amountStars},
	}
	inv := tgbotapi.NewInvoice(
		chatID,
		"🔐 Premium VPN доступ",
		"С подпиской вы получаете:\n🎯 Полный доступ ко всем серверам\n⚡ Максимальная скорость\n📞 Круглосуточная поддержка\n♾️ Любое количество устройств\n🔄 Легкое продление",
		"order_"+strconv.Itoa(amountStars),
		"",
		"",
		"XTR",
		prices,
	)

	// добавь строку:
	inv.SuggestedTipAmounts = []int{}

	// inv.PhotoURL = "https://img.freepik.com/free-vector/secure-cloud-computing-vector-illustration_53876-76148.jpg"
	// inv.PhotoWidth = 600
	// inv.PhotoHeight = 400

	_, err := bot.Send(inv)
	return err
}

func handlePreCheckout(bot *tgbotapi.BotAPI, pcq *tgbotapi.PreCheckoutQuery) {
	// Тут можно провалидировать payload/сумму/валюту
	ans := tgbotapi.PreCheckoutConfig{
		PreCheckoutQueryID: pcq.ID,
		OK:                 true,
		// ErrorMessage:    "Что-то пошло не так" // если нужно отказать
	}
	if _, err := bot.Request(ans); err != nil {
		log.Printf("precheckout answer error: %v", err)
	}
}

func handleSuccessfulPayment(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, pfsenseClient *pfsense.PfSenseClient) {
	sp := msg.SuccessfulPayment
	if sp == nil {
		return
	}
	if sp.Currency != "XTR" {
		log.Printf("unexpected currency: %s", sp.Currency)
		return
	}
	log.Printf("paid: %d XTR, payload=%s", sp.TotalAmount, sp.InvoicePayload)

	// 👉 здесь выдай доступ: заведи user в pfSense / активируй подписку / пр.
	// ... твоя логика ...
	createUserAndSendCertificate(tgbotapi.Update{Message: msg}, pfsenseClient, bot)

	// Подтверждение пользователю
	_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Оплата получена. Отправляем VPN... ✅"))

	sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d оплатил подписку на VPN!", msg.From.ID), msg.From.UserName, bot)
}

func sendCertificate(certRefID, telegramUserid, certDateUntil string, isProb bool, update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI) {

	var certName string

	if isProb {
		certName = fmt.Sprintf("TrialCert%s", telegramUserid)
	} else {
		certName = fmt.Sprintf("Cert%s", telegramUserid)
	}

	ovpnData, err := pfsenseClient.GenerateOVPN(certRefID, "", "213.21.200.205")
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Error generating OVPN: "+err.Error())
		bot.Send(msg)
	}

	// Отправка OVPN в Telegram как файл
	fileBytes := tgbotapi.FileBytes{
		Name:  certName + ".ovpn",
		Bytes: ovpnData,
	}
	docMsg := tgbotapi.NewDocument(update.Message.Chat.ID, fileBytes)
	docMsg.ReplyToMessageID = update.Message.MessageID
	bot.Send(docMsg)

	// Подтверждение в чате
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Ваш userID:{%d}, отправлен VPN\nИстекает: %s", update.Message.From.ID, certDateUntil))
	msg.ReplyToMessageID = update.Message.MessageID
	bot.Send(msg)

}

func sendStart(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, startText)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = menuKeyboard()

	// if _, err := bot.Send(msg); err != nil {
	// 	log.Println("sendStart error:", err)
	// }

	var row []tgbotapi.InlineKeyboardButton
	row = append(row, tgbotapi.NewInlineKeyboardButtonData("🆓 Пробный доступ", "trial"))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)

	msg.ReplyMarkup = keyboard

	bot.Send(msg)
}

// func sendCertificateURL(certRefID, telegramUserid, certDateUntil string, isProb bool,
// 	update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI) {

// 	var certName string
// 	if isProb {
// 		certName = fmt.Sprintf("TrialCert%s", telegramUserid)
// 	} else {
// 		certName = fmt.Sprintf("Cert%s", telegramUserid)
// 	}

// 	// Генерация ovpn
// 	ovpnData, err := pfsenseClient.GenerateOVPN(certRefID, "", "213.21.200.205")
// 	if err != nil {
// 		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка при генерации OVPN: "+err.Error())
// 		bot.Send(msg)
// 		return
// 	}

// 	// Сохраняем файл
// 	savePath := fmt.Sprintf("/var/www/certs/%s.ovpn", certName)

// 	err = os.WriteFile(savePath, ovpnData, 0600)
// 	if err != nil {
// 		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка при сохранении файла: "+err.Error())
// 		bot.Send(msg)
// 		return
// 	}

// 	// Формируем URL (замени на свой домен!)
// 	url := fmt.Sprintf("http://213.21.200.208/certs/%s.ovpn", certName)

// 	// OpenVPN-линк
// 	openvpnURL := fmt.Sprintf("openvpn://import-config?url=%s", url)

// 	// Отправляем пользователю
// 	text := fmt.Sprintf(
// 		"✅ Ваш VPN готов!\nИстекает: %s\n\nСсылка для импорта:\n%s\n\nЕсли не открывается автоматически, скачайте конфиг тут:\n%s",
// 		certDateUntil, openvpnURL, url,
// 	)

// 	msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
// 	bot.Send(msg)
// }
