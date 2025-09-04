package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	pfsense "github.com/Asort97/vpnBot/clients/pfSense"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	pfsenseApiKey := os.Getenv("PFSENSE_API_KEY")
	botToken := os.Getenv("TG_BOT_TOKEN")

	pfsenseClient := pfsense.New(pfsenseApiKey)
	bot, err := tgbotapi.NewBotAPI(botToken)

	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil { // If we got a message
			log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

			if update.Message.Text == "/createuser" {

				userIdStr := fmt.Sprint(update.Message.From.ID)
				certName := fmt.Sprintf("Sert%s", userIdStr)

				id, _ := pfsenseClient.CreateUser(userIdStr, "123", "", "", false)
				uuid, _ := pfsenseClient.GetCARef()
				_, certRefID, _ := pfsenseClient.CreateCertificate(certName, uuid, "RSA", 2048, "", "sha256", userIdStr)
				pfsenseClient.AttachCertificateToUser(id, certRefID)
				// pfsenseClient.FindCertificate(certID)
				// pfsenseClient.ExportCertificateP12(certRefID, "")
				ovpnData, err := pfsenseClient.GenerateOVPN(certRefID, "", "213.21.200.205")
				if err != nil {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Error generating OVPN: "+err.Error())
					bot.Send(msg)
					continue
				}

				if strings.HasPrefix(update.Message.Text, "/delete") {
					parts := strings.Split(update.Message.Text, " ")
					userIdToDelete := parts[1]
					pfsenseClient.DeleteUserCertificate(userIdToDelete)
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
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "New user created and OVPN sent!")
				msg.ReplyToMessageID = update.Message.MessageID
				bot.Send(msg)

				continue
			}

			if update.Message.Text == "ca" {
				pfsenseClient.GetCARef()
				continue
			}

			if update.Message.Text == "cert" {
				pfsenseClient.FindCertificate("5")
				continue
			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Text)
			msg.ReplyToMessageID = update.Message.MessageID

			bot.Send(msg)
		}
	}
}
