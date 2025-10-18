package yookassa

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
	// tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	// tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	// tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type YooKassaClient struct {
	yookassaShopID    string
	yookassaSecretKey string
}

type YooKassaPaymentRequest struct {
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Capture      bool                   `json:"capture"`
	Confirmation map[string]interface{} `json:"confirmation"`
	Description  string                 `json:"description"`
	Metadata     map[string]interface{} `json:"metadata"`
	Receipt      *Receipt               `json:"receipt,omitempty"` // Добавляем чек для 54-ФЗ
}

type Receipt struct {
	Customer struct {
		Email string `json:"email"`
	} `json:"customer"`
	Items []ReceiptItem `json:"items"`
}

type ReceiptItem struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	Amount      struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	VatCode        int    `json:"vat_code"`
	PaymentMode    string `json:"payment_mode"`
	PaymentSubject string `json:"payment_subject"`
}

type YooKassaPaymentResponse struct {
	ID           string                 `json:"id"`
	Status       string                 `json:"status"`
	Amount       map[string]interface{} `json:"amount"`
	Description  string                 `json:"description"`
	Recipient    map[string]interface{} `json:"recipient"`
	CreatedAt    string                 `json:"created_at"`
	Confirmation map[string]interface{} `json:"confirmation"`
	Paid         bool                   `json:"paid"`
	Refundable   bool                   `json:"refundable"`
	Metadata     map[string]interface{} `json:"metadata"`
	Receipt      *Receipt               `json:"receipt,omitempty"`
}

func New(shopID, apiKey string) *YooKassaClient {

	return &YooKassaClient{
		yookassaShopID:    shopID,
		yookassaSecretKey: apiKey,
	}
}

func (y *YooKassaClient) CreateYooKassaPayment(amount float64, description string, chatID int64, product string, userEmail string) (*YooKassaPaymentResponse, error) {
	paymentReq := YooKassaPaymentRequest{}

	// Форматируем сумму (обязательно в формате "299.00")
	paymentReq.Amount.Value = fmt.Sprintf("%.2f", amount)
	paymentReq.Amount.Currency = "RUB"
	paymentReq.Capture = true

	// ПРАВИЛЬНОЕ подтверждение по документации
	paymentReq.Confirmation = map[string]interface{}{
		"type":       "redirect",
		"return_url": "https://t.me/your_bot", // URL для возврата после оплаты
	}

	paymentReq.Description = description

	// Метаданные должны быть map[string]interface{}
	paymentReq.Metadata = map[string]interface{}{
		"chat_id":  chatID,
		"product":  product,
		"order_id": fmt.Sprintf("order_%d_%d", chatID, time.Now().Unix()),
	}

	// Добавляем чек для 54-ФЗ (если нужен)
	if userEmail != "" {
		paymentReq.Receipt = &Receipt{
			Customer: struct {
				Email string `json:"email"`
			}{Email: userEmail},
			Items: []ReceiptItem{
				{
					Description: description,
					Quantity:    "1.00",
					Amount: struct {
						Value    string `json:"value"`
						Currency string `json:"currency"`
					}{
						Value:    fmt.Sprintf("%.2f", amount),
						Currency: "RUB",
					},
					VatCode:        1, // НДС 20%
					PaymentMode:    "full_payment",
					PaymentSubject: "service",
				},
			},
		}
	}

	jsonData, err := json.Marshal(paymentReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка маршалинга: %v", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST",
		"https://api.yookassa.ru/v3/payments",
		bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %v", err)
	}

	// Basic Auth для Юкассы
	auth := fmt.Sprintf("%s:%s", y.yookassaShopID, y.yookassaSecretKey)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotence-Key", fmt.Sprintf("%d", time.Now().UnixNano()))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ошибка API Юкассы: %s, тело: %s", resp.Status, string(body))
	}

	var paymentResp YooKassaPaymentResponse
	err = json.Unmarshal(body, &paymentResp)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	return &paymentResp, nil
}

// Получение статуса платежа
func (y *YooKassaClient) GetYooKassaPaymentStatus(paymentID string) (*YooKassaPaymentResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET",
		fmt.Sprintf("https://api.yookassa.ru/v3/payments/%s", paymentID),
		nil)
	if err != nil {
		return nil, err
	}

	auth := fmt.Sprintf("%s:%s", y.yookassaShopID, y.yookassaSecretKey)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var paymentResp YooKassaPaymentResponse
	err = json.Unmarshal(body, &paymentResp)
	if err != nil {
		return nil, err
	}

	return &paymentResp, nil
}

// // Отправка сообщения с кнопкой оплаты
// func (y *YooKassaClient) sendYooKassaPaymentButton(bot *tgbotapi.BotAPI, chatID int64, amount float64, productName string, userEmail string) error {
// 	payment, err := y.CreateYooKassaPayment(
// 		amount,
// 		productName,
// 		chatID,
// 		productName,
// 		userEmail,
// 	)
// 	if err != nil {
// 		return fmt.Errorf("ошибка создания платежа: %v", err)
// 	}

// 	// Извлекаем URL для оплаты из confirmation
// 	confirmationURL := ""
// 	if confirmation, ok := payment.Confirmation["confirmation_url"].(string); ok {
// 		confirmationURL = confirmation
// 	} else {
// 		return fmt.Errorf("не удалось получить URL для оплаты")
// 	}

// 	message := fmt.Sprintf(`💎 *%s*

// 💰 Сумма к оплате: *%.2f руб.*
// 📝 Описание: %s

// Нажмите кнопку ниже для перехода к оплате:`,
// 		productName, amount, productName)

// 	msg := tgbotapi.NewMessage(chatID, message)
// 	msg.ParseMode = "Markdown"

// 	// Создаем кнопку со ссылкой на страницу оплаты Юкассы
// 	keyboard := tgbotapi.NewInlineKeyboardMarkup(
// 		tgbotapi.NewInlineKeyboardRow(
// 			tgbotapi.NewInlineKeyboardButtonURL("💳 Оплатить", confirmationURL),
// 		),
// 	)
// 	msg.ReplyMarkup = keyboard

// 	_, err = bot.Send(msg)
// 	return err
// }

// // Для VPN услуги
// func (y *YooKassaClient) SendVPNPayment(bot *tgbotapi.BotAPI, chatID int64, userEmail string) error {
// 	return y.sendYooKassaPaymentButton(bot, chatID, 1.00,
// 		"VPN Premium - доступ на 30 дней", userEmail)
// }
