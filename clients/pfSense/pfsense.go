package pfsense

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	colorfulprint "github.com/Asort97/vpnBot/clients/colorfulPrint"
)

type PfSenseClient struct {
	apiKey string
}

type CertificateRequest struct {
	Certificate struct {
		Method    string `json:"method"`
		Name      string `json:"name"`
		User      string `json:"user"`
		Descr     string `json:"descr"`
		KeyLength int    `json:"key_length"`
		Lifetime  int    `json:"lifetime"`
	} `json:"certificate"`
}

func New(apiKey string) *PfSenseClient {
	return &PfSenseClient{apiKey: apiKey}
}

func (c *PfSenseClient) CreateUser(username, password, fullName, email string, disabled bool) (string, error) {
	url := "https://drake2.eunet.lv/api/v2/user"

	// fmt.Println("Creating user in pfSense...")

	colorfulprint.PrintState("Creating user in pfSense...")

	payload := map[string]interface{}{
		"name":     username, // было username
		"password": password,
		"descr":    fullName, // было full_name
		"email":    email,
		"disabled": disabled,
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return "", colorfulprint.PrintError("failed to marshal json: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", colorfulprint.PrintError("error creating request: %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", colorfulprint.PrintError("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Response: %s\n", string(body))

	var result struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}

	err = json.Unmarshal(body, &result)

	if err != nil {
		return "", colorfulprint.PrintError("error unmarshal json: %w", err)
	}

	if resp.StatusCode >= 400 {
		errorText := fmt.Sprintf("failed with status %s: %s", resp.Status, string(body))
		return "", colorfulprint.PrintError(errorText, nil)
	}

	return strconv.Itoa(result.Data.ID), err
}

func (c *PfSenseClient) CreateCertificate(descr, caref, keytype string, keylen int, ecname, digestAlg, dnCommonName string) (string, string, error) {
	url := "https://drake2.eunet.lv/api/v2/system/certificate/generate"

	colorfulprint.PrintState(fmt.Sprintf("Creating certificate for %s in pfSense...\n", dnCommonName))

	payload := map[string]interface{}{
		"descr":         descr,
		"caref":         caref,
		"keytype":       keytype,      // "RSA" или "ECDSA"
		"digest_alg":    digestAlg,    // например, "sha256"
		"dn_commonname": dnCommonName, // имя пользователя или сертификата
	}

	// Для RSA нужен keylen, для ECDSA — ecname
	if keytype == "RSA" {
		payload["keylen"] = keylen // например, 2048
	} else if keytype == "ECDSA" {
		payload["ecname"] = ecname // например, "prime256v1"
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return "", "", colorfulprint.PrintError("failed to marshal json: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", "", colorfulprint.PrintError("error creating request: %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", colorfulprint.PrintError("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %s\n", resp.Status)
	// fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode >= 400 {
		return "", "", colorfulprint.PrintError(fmt.Sprintf("failed with status %s: %s", resp.Status, string(body)), nil)
	}

	var result struct {
		Data struct {
			ID    int    `json:"id"`
			RefID string `json:"refid"`
			Descr string `json:"descr"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", colorfulprint.PrintError("error parsing json: %w", err)
	}

	colorfulprint.PrintState(fmt.Sprintf("Founded cert id: %d RefId: %s Descr: %s\n", result.Data.ID, result.Data.RefID, result.Data.Descr))

	return strconv.Itoa(result.Data.ID), result.Data.RefID, nil
}

func (c *PfSenseClient) GetCARef() (string, error) {
	colorfulprint.PrintState("Getting CARef...\n")
	url := "https://drake2.eunet.lv/api/v2/system/certificate_authorities"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %w", err)
	}

	// fmt.Printf("CA raw response: %s\n", string(body))

	var result struct {
		Data []struct {
			RefID string `json:"refid"`
			Descr string `json:"descr"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("error parsing json: %w", err)
	}
	if len(result.Data) == 0 {
		return "", fmt.Errorf("no CA found")
	}

	for i := 0; i < len(result.Data); i++ {
		fmt.Printf("CA of %s and uuid %s\n", result.Data[i].Descr, result.Data[i].RefID)
	}

	// Вернём UUID первого CA (или выберите нужный по имени)
	return result.Data[0].RefID, nil
}

func (c *PfSenseClient) AttachCertificateToUser(userId, certId string) error {
	fmt.Printf("Attaching Cert%s to user %s...\n", certId, userId)

	url := "https://drake2.eunet.lv/api/v2/user"
	payload := map[string]interface{}{
		"id":   userId,
		"cert": []string{certId}, // массив строк
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("couldnt marshal payload %w", err)
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("couldnt create request %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("couldnt send request %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %s\n", resp.Status)
	// fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed with status %s: %s", resp.Status, string(body))
	}

	return nil
}

func fixPfxTrailingData(pfxData []byte) []byte {
	// Пытаемся найти конец PFX структуры
	// P12 заканчивается байтами 0x30, 0x00 (или другими в зависимости от кодировки)
	// Ищем последовательность, которая выглядит как конец ASN.1 структуры
	for i := len(pfxData) - 1; i > 0; i-- {
		if pfxData[i] != 0x00 {
			// Возвращаем данные до первого ненулевого байта с конца
			return pfxData[:i+1]
		}
	}
	return pfxData
}

func (c *PfSenseClient) ExportCertificateP12(certRef, passphrase string) ([]byte, error) {
	url := "https://drake2.eunet.lv/api/v2/system/certificate/pkcs12/export"

	payload := map[string]interface{}{
		"certref":    certRef,
		"encryption": "low", // AES256
		"passphrase": passphrase,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/octet-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Проверка, не вернулся ли JSON с ошибкой
	if resp.Header.Get("Content-Type") != "application/octet-stream" || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("unexpected response: status=%s, body=%s", resp.Status, string(data))
	}

	fixedData := fixPfxTrailingData(data)

	os.WriteFile("debug_fixed.p12", fixedData, 0600)
	return fixedData, nil

}

func (c *PfSenseClient) FindCertificate(refid string) {
	colorfulprint.PrintState(fmt.Sprintf("Looking for certificate: %s", refid))

	url := fmt.Sprintf("https://drake2.eunet.lv/api/v2/system/certificate?id=%s", refid)

	var CertDetail struct {
		Data struct {
			Crt   string `json:"crt"`
			Prv   string `json:"prv"`
			CaRef string `json:"caref"`
		} `json:"data"`
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		colorfulprint.PrintError("error to make request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		colorfulprint.PrintError("error to get response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		colorfulprint.PrintError(fmt.Sprintf("failed with status %s: %s", resp.Status, string(body)), nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		colorfulprint.PrintError("error reading response: %w", err)
	}

	err = json.Unmarshal(body, &CertDetail)
	if err != nil {
		colorfulprint.PrintError("error unmarshal json: %w", err)
	}

	fmt.Printf("Response status %s\n", string(body))
	fmt.Printf("Private key: %s\n", CertDetail.Data.Prv)
}

func ParseP12WithOpenSSL(p12Data []byte, passphrase string) (certPEM, keyPEM, caPEM []byte, err error) {
	// Создаём временный файл
	tmpFile, err := os.CreateTemp("", "cert_*.p12")
	if err != nil {
		return nil, nil, nil, err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(p12Data); err != nil {
		return nil, nil, nil, err
	}
	tmpFile.Close()

	// Команда для извлечения всей цепочки в PEM
	cmd := exec.Command("C:\\Program Files\\OpenSSL-Win64\\bin\\openssl.exe", "pkcs12",
		"-in", tmpFile.Name(),
		"-nodes", // не шифровать приватный ключ
		"-passin", "pass:"+passphrase,
	)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, nil, nil, fmt.Errorf("openssl failed: %v\nstderr: %s", err, stderr.String())
	}

	// Парсим вывод openssl
	pemData := out.Bytes()
	return ParsePEMChain(pemData)
}

// Функция для парсинга вывода openssl
func ParsePEMChain(pemData []byte) (certPEM, keyPEM, caPEM []byte, err error) {
	var blocks []*pem.Block
	rest := pemData

	// Извлекаем все PEM-блоки
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		blocks = append(blocks, block)
	}

	// Распределяем блоки по типам
	for _, block := range blocks {
		pemBytes := pem.EncodeToMemory(block)
		switch {
		case strings.Contains(block.Type, "PRIVATE KEY"):
			keyPEM = append(keyPEM, pemBytes...)
		case strings.Contains(block.Type, "CERTIFICATE"):
			// Первый сертификат - клиентский, остальные - цепочка CA
			if certPEM == nil {
				certPEM = pemBytes
			} else {
				caPEM = append(caPEM, pemBytes...)
			}
		}
	}

	if certPEM == nil || keyPEM == nil {
		return nil, nil, nil, fmt.Errorf("failed to extract cert or key from PEM chain")
	}

	return certPEM, keyPEM, caPEM, nil
}

// func ParseP12(data []byte, passphrase string) (certPEM, keyPEM, caPEM []byte, err error) {
// 	blocks, err := pkcs12.ToPEM(data, passphrase)
// 	if err != nil {
// 		return nil, nil, nil, fmt.Errorf("failed to decode p12: %w", err)
// 	}

// 	var keyBlock, clientCertBlock *pem.Block
// 	var caBlocks []*pem.Block

// 	// Проходим по всем PEM-блокам и сортируем их
// 	for _, block := range blocks {
// 		switch {
// 		case strings.Contains(block.Type, "PRIVATE KEY"):
// 			// Нашли приватный ключ
// 			keyBlock = block
// 		case strings.Contains(block.Type, "CERTIFICATE"):
// 			// Это сертификат. Нужно определить, клиентский он или CA.
// 			// Простейшая эвристика: клиентский сертификат обычно имеет CommonName = username.
// 			// Более надежный способ - посмотреть в поле Subject.
// 			// Для начала просто будем считать первый сертификат - клиентским, остальные - CA.
// 			if clientCertBlock == nil {
// 				clientCertBlock = block
// 			} else {
// 				caBlocks = append(caBlocks, block)
// 			}
// 		}
// 	}

// 	// Преобразуем блоки в байты
// 	if keyBlock != nil {
// 		keyPEM = pem.EncodeToMemory(keyBlock)
// 	}
// 	if clientCertBlock != nil {
// 		certPEM = pem.EncodeToMemory(clientCertBlock)
// 	}
// 	for _, block := range caBlocks {
// 		caPEM = append(caPEM, pem.EncodeToMemory(block)...)
// 	}

// 	// Проверяем, что нашли самое необходимое
// 	if certPEM == nil || keyPEM == nil {
// 		return nil, nil, nil, fmt.Errorf("p12 does not contain client cert or key")
// 	}
// 	// CA может и не быть в P12, если система доверяет ему иначе, но у нас он обычно есть.

//		return certPEM, keyPEM, caPEM, nil
//	}
//
// ensureNL: trim и гарантируем перевод строки в конце
func ensureNL(b []byte) []byte {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	return b
}

func (c *PfSenseClient) GenerateOVPN(certRef, passphrase, server string) ([]byte, error) {
	// 1) Экспорт PKCS#12
	p12Data, err := c.ExportCertificateP12(certRef, passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to export PKCS#12: %w", err)
	}
	if len(p12Data) == 0 {
		return nil, fmt.Errorf("empty PKCS#12 export")
	}

	// 2) Разбор P12 → PEM ([]byte)
	certPEM, keyPEM, caPEM, err := ParseP12WithOpenSSL(p12Data, passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS#12: %w", err)
	}

	// 3) Нормализация PEM-блоков
	caPEM = ensureNL(caPEM)
	certPEM = ensureNL(certPEM)
	keyPEM = ensureNL(keyPEM)

	// 4) TLS-Crypt ключ (байты) — ДОЛЖЕН совпадать с серверным ключом!
	tlsCrypt := ensureNL([]byte(`# 
# 2048 bit OpenVPN static key
#
-----BEGIN OpenVPN Static key V1-----
c919a4c433763d30341eb30f9922a640
eb86233d877cff37f87fbe14b9056fbc
476bf366387c23b94d9022375fcdcac7
e91347d0997612ee958fdf61826abdb8
dd96f171ba0be8e6e5657d461c9a59b9
153b23d99abf40a245e2e16ea79cbbcb
eeecd8dfd032cb56e4c020fa933d5193
b7f2cf9ac9fdc21f0deef45856d4df34
617145705d07fdb69b08de1a8cf3fa7f
7c00d257a5b30f1b7df0af6e399868d8
0a5bf5757f772c8315ed98da0abcd77a
7de142991fc7f68d8a39e367216d1e7e
7b866d6ac11a71228324875ffbb3e76b
1fc6d8e78a5acf655644533b7d3c97d6
ba07ecf5733c3349a88aa8a1be49cde2
0e24ba2abced5ad4692413297b933133
-----END OpenVPN Static key V1-----`))

	var buf bytes.Buffer

	// 5) Заголовок конфигурации (совм. 2.4–2.6)
	fmt.Fprintf(&buf, `dev tun
proto udp4
persist-tun
persist-key
data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305:AES-256-CBC
data-ciphers-fallback AES-256-CBC
cipher AES-256-CBC
auth SHA512
tls-client
client
resolv-retry infinite
remote %s 1443 udp4
nobind
verify-x509-name "drake2-sc" name
remote-cert-tls server
explicit-exit-notify 1

`, server)

	// 6) Вкладываем PEM-блоки (важно: закрывающий тег с новой строки)
	buf.WriteString("<ca>\n")
	buf.Write(caPEM)
	buf.WriteString("</ca>\n")

	buf.WriteString("<cert>\n")
	buf.Write(certPEM)
	buf.WriteString("</cert>\n")

	buf.WriteString("<key>\n")
	buf.Write(keyPEM)
	buf.WriteString("</key>\n")

	buf.WriteString("<tls-crypt>\n")
	buf.Write(tlsCrypt)
	buf.WriteString("</tls-crypt>\n")

	return buf.Bytes(), nil
}

func (c *PfSenseClient) DeleteUserCertificate(certificateId string) error {
	url := fmt.Sprintf("https://drake2.eunet.lv/api/v2/system/certificate?id=%s", certificateId)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return colorfulprint.PrintError("Cant request", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return colorfulprint.PrintError("error sending request: %w", err)
	}
	defer resp.Body.Close()

	return nil
}
