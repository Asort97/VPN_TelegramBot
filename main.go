package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	colorfulprint "github.com/Asort97/vpnBot/clients/colorfulPrint"
	pfsense "github.com/Asort97/vpnBot/clients/pfSense"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
				handleSuccessfulPayment(bot, update.Message)
				continue
			}

			if update.Message.IsCommand() && update.Message.Command() == "start" {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Меню:")
				msg.ReplyMarkup = menuKeyboard()
				bot.Send(msg)
				continue
			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Меню:")
			msg.ReplyMarkup = menuKeyboard()

			switch update.Message.Text {
			case "Удалить":
				pfsenseClient.DeleteUserCertificate("4")
			case "Получить VPN":
				amount := 0
				_ = sendStarsInvoice(bot, update.Message.Chat.ID, amount)
			case "Инструкция по использованию":
				instructionWindows(update, bot)
			// case "Оплатить":
			// 	amount := 0
			// 	_ = sendStarsInvoice(bot, update.Message.Chat.ID, amount)
			case "Проверить статус":
				checkStatus(pfsenseClient, update, bot)
			}
		}
	}
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

	from, until, expired, err := pfsenseClient.GetDateOfCertificate(certId)
	if err != nil {
		colorfulprint.PrintError(fmt.Sprintf("Couldnt get date of certificate{%s}\n", certId), err)
	}

	var status string

	if expired {
		status = "Истек"
	} else {
		status = "Работает"
	}

	text := fmt.Sprintf("Ваш подписка оплачена с %s и длится до %s\nСтатус работы: %s", from, until, status)
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
	bot.Send(msg)
}

func instructionWindows(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	photo1 := tgbotapi.NewPhoto(update.Message.Chat.ID, tgbotapi.FilePath("InstructionPhotos/Windows/1.png"))
	photo1.Caption = "1) Скачайте <a href=\"https://openvpn.net/community/\">OpenVPN</a> с официального сайта \n"
	photo2 := tgbotapi.NewPhoto(update.Message.Chat.ID, tgbotapi.FilePath("InstructionPhotos/Windows/2.png"))
	photo2.Caption = "2) После скачивания откройте трей в правом нижнем углу \n"
	photo3 := tgbotapi.NewPhoto(update.Message.Chat.ID, tgbotapi.FilePath("InstructionPhotos/Windows/3.png"))
	photo3.Caption = "3) Нажмите правой кнопкой мыши по значку OpenVPN и далее Импорт->Импорт файла конфигурации и выберите файл конфигурации который мы вам отправим\n"
	photo4 := tgbotapi.NewPhoto(update.Message.Chat.ID, tgbotapi.FilePath("InstructionPhotos/Windows/4.png"))
	photo4.Caption = "4) Далее нажмите правой кнопкой по значку снова и нажмите кнопку Подключиться\n"
	photo1.ParseMode = "HTML"
	bot.Send(photo1)
	bot.Send(photo2)
	bot.Send(photo3)
	bot.Send(photo4)
}

func menuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Получить VPN"),
			tgbotapi.NewKeyboardButton("Проверить статус"),
			// tgbotapi.NewKeyboardButton("Оплатить"),
			tgbotapi.NewKeyboardButton("Инструкция по использованию"),
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
		amount := 0
		_ = sendStarsInvoice(bot, update.Message.Chat.ID, amount)

		fmt.Printf("User{%s} is not exist!\n", telegramUserid)
		userID, _ = pfsenseClient.CreateUser(telegramUserid, "123", "", "", false)
	}

	var certRefID string
	var certID string

	_, certRefID, err := pfsenseClient.GetAttachedCertRefIDByUserName(telegramUserid)

	if err != nil {
		colorfulprint.PrintError(fmt.Sprintf("Couldnt find attached certificate on user{%s}\n", telegramUserid), err)

		uuid, _ := pfsenseClient.GetCARef()
		certID, certRefID, _ = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, "", "sha256", telegramUserid)
		pfsenseClient.AttachCertificateToUser(userID, certRefID)
	} else {
		certID, _ = pfsenseClient.GetCertificateIDByRefid(certRefID)
		_, _, expired, _ := pfsenseClient.GetDateOfCertificate(certID)

		if expired {
			// Логика удаления !!!!!!!!!
			pfsenseClient.DeleteUserCertificate(certID)
			//После удаления создаем новый сертификат и привязываем его к пользователю
			uuid, _ := pfsenseClient.GetCARef()
			certID, certRefID, _ = pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, "", "sha256", telegramUserid)
			pfsenseClient.AttachCertificateToUser(userID, certRefID)

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Создан новый сертификат!")
			bot.Send(msg)
		}
	}

	// id, err := pfsenseClient.CreateUser(userIdStr, "123", "", "", false)
	// if err != nil {
	// 	fmt.Printf("Couldnt create user, trying to find existing...")
	// 	id, _, _ = pfsenseClient.GetAttachedCertRedIDByUserName(fmt.Sprint(update.Message.From.ID))
	// }

	// uuid, _ := pfsenseClient.GetCARef()
	// certID, certRefID, _ := pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, "", "sha256", telegramUserid)
	// pfsenseClient.AttachCertificateToUser(id, certRefID)

	_, certDateUntil, _, _ := pfsenseClient.GetDateOfCertificate(certID)

	// pfsenseClient.ExportCertificateP12(certRefID, "")
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

func sendStarsInvoice(bot *tgbotapi.BotAPI, chatID int64, amountStars int) error {
	// if amountStars <= 0 {
	// 	amountStars = 1
	// }
	prices := []tgbotapi.LabeledPrice{
		{Label: "VPN доступ", Amount: amountStars}, // РОВНО один LabeledPrice
	}
	inv := tgbotapi.NewInvoice(
		chatID,
		"VPN доступ",
		"Доступ к VPN конфигу для OpenVPN",
		"order_"+strconv.Itoa(amountStars),
		"",
		"",
		"XTR",
		prices,
	)

	// добавь строку:
	inv.SuggestedTipAmounts = []int{}

	// Можно картинку (не обязательно)
	// inv.PhotoURL = "https://picsum.photos/seed/vpn/600/400"

	// никакие NeedName/NeedEmail не нужны для цифровых
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

func handleSuccessfulPayment(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
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
	createUserAndSendCertificate(tgbotapi.Update{}, &pfsense.PfSenseClient{}, bot)

	// Подтверждение пользователю
	_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Оплата получена. Отправляем VPN... ✅"))
}
