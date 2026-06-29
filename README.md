# Portsari

A Go Telegram bot that provides Telegram-based access to a Yale Doorman L3
smart lock connected via Bluetooth to a Home Assistant VM. The bot talks to HA
exclusively over MQTT and never holds HA credentials.

## Prerequisites

Before running the bot, the following must be set up in HA:

1. Mosquitto add-on running with a `portsari` MQTT user.
2. `yalexs_ble` integration paired with the lock.
3. `mqtt_statestream` configured in `configuration.yaml`.
4. A command-bridge automation subscribing to `portsari/cmd/lock`.

## Quick Start

Build the binary:

```bash
go build -o portsari ./cmd/portsari
```

Copy `deploy/config.yaml.example` to `/etc/portsari/config.yaml` and fill in
your MQTT credentials and Telegram bot token. Set
`access.bootstrap_admin_telegram_id` to your own Telegram user ID for the
first run — this creates the first admin account on `/start`. After the first
admin exists, remove or set this field to `0`.

```yaml
telegram:
  token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

access:
  bootstrap_admin_telegram_id: 123456789    # your Telegram ID
```

Run directly:

```bash
./portsari --config /etc/portsari/config.yaml
```

### Systemd Installation

The [deploy/](./deploy/) directory contains a systemd unit and install script:

```bash
sudo bash deploy/deploy.sh
```

This builds the binary, installs it to `/usr/local/bin/portsari`, installs the
systemd unit, and starts the service. The bot runs as the `portsari` user.

The systemd unit expects the config at `/etc/portsari/config.yaml`.

## Configuration Reference

See [deploy/config.yaml.example](./deploy/config.yaml.example) for a complete
file. Required fields:

| Section | Key | Description |
|---|---|---|
| `mqtt` | `broker` | MQTT broker address (`host:port`) |
| `mqtt` | `username` | MQTT username |
| `mqtt` | `password` | MQTT password |
| `telegram` | `token` | Bot token from [@BotFather](https://t.me/BotFather) |
| `database` | `path` | Path to SQLite database file |
| `access` | `bootstrap_admin_telegram_id` | Your Telegram ID (numeric) for first admin creation on first `/start`. **Remove or set to `0` after the first admin account is created.** |

## User Flow

- **Regular users** get a persistent keyboard with a single "Unlock" button.
  Tapping it or sending `/unlock` unlocks the door (subject to tier and rate
  limits).
- **Admins** get the Unlock button plus an "Admin" button that opens an inline
  menu with: Status, Users (paged list with role/tier/remove actions), Invite
  (preset codes + revoke), Full log, and Broadcast.
- All slash commands have inline-keyboard equivalents and vice versa.

Admin commands (fallback surface):

| Command | Action |
|---|---|
| `/lock` | Lock the door |
| `/unlock` | Unlock the door |
| `/status` | Lock state, battery, last event |
| `/users` | List users with role/tier/status |
| `/invite <role> <tier> [duration]` | Create an invite code |
| `/revoke <code>` | Revoke an unused invite |
| `/setrole <id> <role>` | Change a user's role |
| `/settier <id> <tier>` | Change a user's tier |
| `/remove <id>` | Deactivate a user |
| `/log [n]` | Show last n access log entries |
| `/broadcast <msg>` | Message all active users |

Regular users do not see status, log, or management commands.

## Architecture Overview

```
Telegram Users ←→ Go Bot ←→ Mosquitto (HA VM) ←→ Yale L3 (BLE)
```

The bot subscribes to HA state via `mqtt_statestream` and publishes lock
commands to a bot-owned MQTT topic. A single HA automation bridges command
topics to `lock.lock` / `lock.unlock` service calls.

## Proactive Alerts

The bot watches lock state and sensor topics and sends alerts for: unlocked,
locked, jammed, battery low/critical, lock offline, and back online. Alerts
are routed to all users or admins depending on severity.

## License

MIT
