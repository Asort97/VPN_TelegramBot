#!/bin/bash

cd "$(dirname "$0")"

export PFSENSE_API_KEY=19c5dd8bd082e5eaf4eab46e23e04ccc
export TG_BOT_TOKEN=8482545787:AAHN9ifSMPO_UDleeVtVRSz5_RMNmjzdQGE
export TLS_CRYPT_KEY=Start/tls.key
export INVOICE_TOKEN=390540012:LIVE:80216
export INVOICE_TOKEN_TEST=381764678:TEST:138753
export YOOKASSA_API_KEY=live_PxUSryvQ3iLakKGc0181rEH-UCvfEsz0dqS7tMk5jVw
export YOOKASSA_STORE_ID=1184429
/usr/local/go/bin/go run main.go
