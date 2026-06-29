#!/usr/bin/env bash
set -euo pipefail

BINARY="portsari"
SERVICE="portsari.service"
BIN_PATH="/usr/local/bin/${BINARY}"
SVC_PATH="/etc/systemd/system/${SERVICE}"

cd "$(dirname "$0")/.."

echo "Building ${BINARY}..."
go build -o "${BINARY}" ./cmd/portsari

echo "Installing binary to ${BIN_PATH}..."
sudo cp "${BINARY}" "${BIN_PATH}"

echo "Installing systemd unit to ${SVC_PATH}..."
sudo cp "deploy/${SERVICE}" "${SVC_PATH}"

echo "Reloading systemd..."
sudo systemctl daemon-reload

echo "Enabling ${SERVICE}..."
sudo systemctl enable "${SERVICE}"

echo "Restarting ${SERVICE}..."
sudo systemctl restart "${SERVICE}"

echo "Done."
