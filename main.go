package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	colorfulprint "github.com/Asort97/vpnBot/clients/colorfulPrint"
	instruct "github.com/Asort97/vpnBot/clients/instruction"
	pfsense "github.com/Asort97/vpnBot/clients/pfSense"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

const vpnCost int = 100

var invoiceToken string
var invoiceTokenTest string

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
	invoiceToken = os.Getenv("INVOICE_TOKEN")
	invoiceTokenTest = os.Getenv("INVOICE_TOKEN_TEST")
	tlsBytes, _ := os.ReadFile(tlsKey)

	pfsenseClient := pfsense.New(pfsenseApiKey, []byte(tlsBytes))

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

		if update.Message != nil {

			if update.Message.SuccessfulPayment != nil {
				handleSuccessfulPayment(bot, update.Message, pfsenseClient)
				continue
			}

			if update.Message.IsCommand() && update.Message.Command() == "start" {
				sendStart(bot, update.Message.Chat.ID)
				sendMenuKeyboard(bot, update.Message.Chat.ID)
				continue
			}

			if update.Message.Command() == "renew" {
				_, refId, err := pfsenseClient.GetAttachedCertRefIDByUserName(fmt.Sprint(update.Message.From.ID))
				if err != nil {
					colorfulprint.PrintError("ERROR RENEW", err)
				}
				pfsenseClient.RenewExistingCertificateByRefid(refId)
			}

			switch update.Message.Text {
			case "🔑 Получить VPN":
				OnGetVPNButton(bot, update, pfsenseClient)
				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Получить VPN...", update.Message.From.ID), update.Message.From.UserName, bot)
				continue

			case "📖 Инструкция":
				instruct.SendInstructMenu(bot, update.Message.Chat.ID)
				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Инструкции...", update.Message.From.ID), update.Message.From.UserName, bot)
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
				messageWait, _ := bot.Send(msgWait)

				if !canProceedKey(update.Message.From.ID, "get_vpn_trial", 5*time.Second) {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "⏳ Подождите ~5 сек перед повторной выдачей VPN"))
					break
				}

				createProbCertificate(update, pfsenseClient, bot, messageWait.MessageID)
				sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d нажал на кнопку Пробного доступа...", update.Message.From.ID), update.Message.From.UserName, bot)

				continue

			}

			sendMenuKeyboard(bot, update.Message.Chat.ID)
		}

		if cq := update.CallbackQuery; cq != nil && cq.Message != nil {

			chatID := cq.Message.Chat.ID

			if strings.HasPrefix(cq.Data, "win_prev_") {
				// Обработка кнопки "Назад"
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "win_prev_"))
				newStep := currentStep - 1
				instruct.InstructionWindows(chatID, bot, newStep)

			} else if strings.HasPrefix(cq.Data, "win_next_") {
				// Обработка кнопки "Вперед"
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "win_next_"))
				newStep := currentStep + 1
				instruct.InstructionWindows(chatID, bot, newStep)
			}

			if strings.HasPrefix(cq.Data, "android_prev_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "android_prev_"))
				instruct.InstructionAndroid(chatID, bot, currentStep-1)
			} else if strings.HasPrefix(cq.Data, "android_next_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "android_next_"))
				instruct.InstructionAndroid(chatID, bot, currentStep+1)
			}

			// Обработка iOS
			if strings.HasPrefix(cq.Data, "ios_prev_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "ios_prev_"))
				instruct.InstructionIos(chatID, bot, currentStep-1)
			} else if strings.HasPrefix(cq.Data, "ios_next_") {
				currentStep, _ := strconv.Atoi(strings.TrimPrefix(cq.Data, "ios_next_"))
				instruct.InstructionIos(chatID, bot, currentStep+1)
			}

			switch cq.Data {
			case "windows":
				instruct.SetInstructKeyboard(cq.Message.MessageID, chatID, instruct.Windows)
				instruct.InstructionWindows(chatID, bot, 0)
			case "android":
				instruct.SetInstructKeyboard(cq.Message.MessageID, chatID, instruct.Android)
				instruct.InstructionAndroid(chatID, bot, 0)
			case "ios":
				instruct.SetInstructKeyboard(cq.Message.MessageID, chatID, instruct.IOS)
				instruct.InstructionIos(chatID, bot, 0)
			case "trial":
				msgWait := tgbotapi.NewMessage(update.Message.Chat.ID, "Пожалуйста подождите...")
				messageWait, _ := bot.Send(msgWait)
				createProbCertificate(update, pfsenseClient, bot, messageWait.MessageID)
			}
			bot.Request(tgbotapi.NewCallback(cq.ID, ""))
		}
	}
}

func OnGetVPNButton(bot *tgbotapi.BotAPI, update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient) error {
	if !canProceedKey(update.Message.From.ID, "get_vpn", 5*time.Second) {
		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "⏳ Подождите ~5 сек перед повторной выдачей VPN"))
		return colorfulprint.PrintError("ReturnError", nil)
	}

	msgWait := tgbotapi.NewMessage(update.Message.Chat.ID, "Пожалуйста подождите...")
	messageWait, _ := bot.Send(msgWait)

	telegramUserid := fmt.Sprint(update.Message.From.ID)
	_, isExist := pfsenseClient.IsUserExist(telegramUserid)

	if isExist {
		_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUserid)

		if err != nil {
			createUserAndSendCertificate(update, pfsenseClient, bot, messageWait.MessageID)
		} else {
			certID, _ := pfsenseClient.GetCertificateIDByRefid(certRefID)
			// _, _, _, expired, _ := pfsenseClient.GetDateOfCertificate(certID)
			expired := true
			if expired {
				msg := tgbotapi.NewEditMessageText(update.Message.Chat.ID, messageWait.MessageID, "Ваша подписка истекла! Чтобы получить VPN пожалуйста обновите подписку!")
				bot.Send(msg)

				_ = sendStarsInvoice(bot, update.Message.Chat.ID, vpnCost)
			} else {
				_, certDateUntil, _, _, _ := pfsenseClient.GetDateOfCertificate(certID)

				sendCertificate(certRefID, telegramUserid, certDateUntil, false, update, pfsenseClient, bot, messageWait.MessageID)
			}
		}
	} else {
		msg := tgbotapi.NewEditMessageText(update.Message.Chat.ID, messageWait.MessageID, "Чтобы получить VPN оплатите подписку!")
		bot.Send(msg)

		_ = sendStarsInvoice(bot, update.Message.Chat.ID, vpnCost)
	}
	return colorfulprint.PrintError("ReturnError", nil)
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

func createUserAndSendCertificate(update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI, messageIDtoEdit int) {

	telegramUserid := fmt.Sprint(update.Message.From.ID)
	certName := fmt.Sprintf("Cert%s", telegramUserid)

	// var userID string
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
		// _, _, _, expired, _ := pfsenseClient.GetDateOfCertificate(certID)
		expired := true
		if expired {
			// Логика удаления !!!!!!!!!
			// pfsenseClient.DeleteUserCertificate(certID)
			// //После удаления создаем новый сертификат и привязываем его к пользователю
			// uuid, _ := pfsenseClient.GetCARef()
			// certID, certRefID, _ = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, 30, "", "sha256", telegramUserid)
			// pfsenseClient.AttachCertificateToUser(userID, certRefID)

			_, refId, err := pfsenseClient.GetAttachedCertRefIDByUserName(fmt.Sprint(update.Message.From.ID))
			if err != nil {
				colorfulprint.PrintError("ERROR RENEW", err)
			}
			pfsenseClient.RenewExistingCertificateByRefid(refId)

			// msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Создан новый сертификат!")
			// bot.Send(msg)
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

	if messageIDtoEdit != 0 {
		media := tgbotapi.NewInputMediaDocument(fileBytes)
		media.Caption = fmt.Sprintf("Создан новый пользователь с userID:{%d} и отправлен VPN\nИстекает: %s", update.Message.From.ID, certDateUntil)

		edit := tgbotapi.EditMessageMediaConfig{
			BaseEdit: tgbotapi.BaseEdit{
				ChatID:    update.Message.Chat.ID,
				MessageID: messageIDtoEdit,
			},
			Media: media,
		}

		bot.Send(edit)
	} else {
		docMsg := tgbotapi.NewDocument(update.Message.Chat.ID, fileBytes)
		docMsg.ReplyToMessageID = update.Message.MessageID
		bot.Send(docMsg)

		// Подтверждение в чате
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Создан новый пользователь с userID:%d и отправлен VPN\nИстекает: %s", update.Message.From.ID, certDateUntil))
		msg.ReplyToMessageID = update.Message.MessageID
		bot.Send(msg)
	}
}

func createProbCertificate(update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI, messageIDtoEdit int) {
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

		sendCertificate(certRefID, telegramUserid, certDateUntil, true, update, pfsenseClient, bot, messageIDtoEdit)
		return
	}

	_, certDateUntil, _, expired, _ := pfsenseClient.GetDateOfCertificate(certID)
	if expired {
		if messageIDtoEdit != 0 {
			edit := tgbotapi.NewEditMessageText(chatID, messageIDtoEdit, "Ваш пробный период подошел к концу 😊.\nНо это только начало! Продолжите пользоваться всеми преимуществами полного доступа")
			bot.Send(edit)
		} else {
			msg := tgbotapi.NewMessage(chatID, "Ваш пробный период подошел к концу 😊.\nНо это только начало! Продолжите пользоваться всеми преимуществами полного доступа!")
			bot.Send(msg)
		}
		return
	}

	sendCertificate(certRefID, telegramUserid, certDateUntil, true, update, pfsenseClient, bot, messageIDtoEdit)
}

func sendStarsInvoice(bot *tgbotapi.BotAPI, chatID int64, amountStars int) error {

	prices := []tgbotapi.LabeledPrice{
		{Label: "VPN Premium на 30 дней", Amount: amountStars * 100},
	}

	payload := fmt.Sprintf("order_%d_%d", chatID, time.Now().Unix())

	inv := tgbotapi.NewInvoice(
		chatID,
		"🔐 Premium VPN доступ",
		"С подпиской вы получаете:\n🎯 Полный доступ ко всем серверу\n⚡ Максимальная скорость\n📞 Круглосуточная поддержка\n♾️ Любое количество устройств\n🔄 Легкое продление",
		payload,
		invoiceTokenTest,
		"",
		"RUB",
		prices,
	)

	// inv.ParseMode = "HTML"
	// добавь строку:
	inv.SuggestedTipAmounts = []int{}
	inv.NeedEmail = true
	inv.SendEmailToProvider = true

	inv.ProviderData = fmt.Sprintf(`{
        "need_email": true,
        "send_email_to_provider": true,
        "provider_data": {
            "receipt": {
                "items": [
                    {
                        "description": "VPN Premium подписка на 30 дней",
                        "quantity": 1,
                        "amount": {
                            "value": %d,
                            "currency": "RUB"
                        },
                        "vat_code": 1,
                        "payment_mode": "full_payment",
                        "payment_subject": "service"
                    }
                ],
                "tax_system_code": 1
            }
        }
    }`, amountStars*100)

	inv.PhotoURL = "https://i.postimg.cc/GmKY3Y2w/REVOLUTION-ICON-NEW.png"
	inv.PhotoWidth = 200
	inv.PhotoHeight = 200

	msg, err := bot.Send(inv)
	log.Printf("Invoice: %+v", msg)
	if err != nil {
		log.Printf("Error: %+v", err)
	}

	// _, err := bot.Send(inv)
	return err
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

func handleSuccessfulPayment(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, pfsenseClient *pfsense.PfSenseClient) {
	sp := msg.SuccessfulPayment
	if sp == nil {
		return
	}
	if sp.Currency != "XTR" && sp.Currency != "RUB" {
		log.Printf("unexpected currency: %s", sp.Currency)
		return
	}
	log.Printf("paid: %d XTR, payload=%s", sp.TotalAmount, sp.InvoicePayload)

	messageWait, _ := bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Оплата получена. Отправляем VPN... ✅"))

	// 👉 здесь выдай доступ: заведи user в pfSense / активируй подписку / пр.
	// ... твоя логика ...
	createUserAndSendCertificate(tgbotapi.Update{Message: msg}, pfsenseClient, bot, messageWait.MessageID)

	// Подтверждение пользователю

	sendMessageToAdmin(fmt.Sprintf("Юзер с id:%d оплатил подписку на VPN!", msg.From.ID), msg.From.UserName, bot)
}

func sendCertificate(certRefID, telegramUserid, certDateUntil string, isProb bool, update tgbotapi.Update, pfsenseClient *pfsense.PfSenseClient, bot *tgbotapi.BotAPI, messageIDtoEdit int) {

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

	if messageIDtoEdit != 0 {
		media := tgbotapi.NewInputMediaDocument(fileBytes)
		media.Caption = fmt.Sprintf("Ваш userID:{%d}\n Ваша подписка истекает: %s", update.Message.From.ID, certDateUntil)

		edit := tgbotapi.EditMessageMediaConfig{
			BaseEdit: tgbotapi.BaseEdit{
				ChatID:    update.Message.Chat.ID,
				MessageID: messageIDtoEdit,
			},
			Media: media,
		}

		bot.Send(edit)
	} else {
		docMsg := tgbotapi.NewDocument(update.Message.Chat.ID, fileBytes)
		docMsg.ReplyToMessageID = update.Message.MessageID
		docMsg.Caption = fmt.Sprintf("Ваш userID:{%d}, отправлен VPN\nИстекает: %s", update.Message.From.ID, certDateUntil)
		bot.Send(docMsg)
	}

	// Подтверждение в чате
	// msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Ваш userID:{%d}, отправлен VPN\nИстекает: %s", update.Message.From.ID, certDateUntil))
	// msg.ReplyToMessageID = update.Message.MessageID
	// bot.Send(msg)

}

func sendStart(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, startText)
	msg.ParseMode = "HTML"

	var row []tgbotapi.InlineKeyboardButton
	row = append(row, tgbotapi.NewInlineKeyboardButtonData("🆓 Пробный доступ", "trial"))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)

	msg.ReplyMarkup = keyboard

	bot.Send(msg)
}

func sendMenuKeyboard(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Воспользуйтесь меню под клавиатурой 👇")
	msg.ReplyMarkup = menuKeyboard()

	bot.Send(msg)
}
