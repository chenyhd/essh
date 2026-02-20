# essh - Encrypted SSH Client

Store SSH server credentials with encrypted passwords, connect by server name.

## Install

### macOS (Homebrew)

```bash
brew install chenyhd/tap/essh
```

### Linux (APT repository)

```bash
curl -fsSL https://chenyhd.github.io/apt-repo/gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/essh.gpg
echo "deb [signed-by=/usr/share/keyrings/essh.gpg] https://chenyhd.github.io/apt-repo stable main" | sudo tee /etc/apt/sources.list.d/essh.list
sudo apt update
sudo apt install essh
```

### Linux (deb)

```bash
# amd64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_amd64.deb
sudo dpkg -i essh_linux_amd64.deb

# arm64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_arm64.deb
sudo dpkg -i essh_linux_arm64.deb
```

### Linux (rpm)

```bash
# amd64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_amd64.rpm
sudo rpm -i essh_linux_amd64.rpm

# arm64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_arm64.rpm
sudo rpm -i essh_linux_arm64.rpm
```

### Linux / macOS (tar.gz)

```bash
# Linux amd64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_amd64.tar.gz

# Linux arm64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_arm64.tar.gz

# macOS Apple Silicon
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_darwin_arm64.tar.gz

# macOS Intel
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_darwin_amd64.tar.gz

tar xzf essh_*.tar.gz
sudo mv essh /usr/local/bin/
```

### Windows (PowerShell)

```powershell
# amd64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_windows_amd64.zip
Expand-Archive essh_windows_amd64.zip -DestinationPath "$env:LOCALAPPDATA\essh"

# arm64
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_windows_arm64.zip
Expand-Archive essh_windows_arm64.zip -DestinationPath "$env:LOCALAPPDATA\essh"

# Create symlink in a directory already in PATH
New-Item -ItemType SymbolicLink -Path "$env:LOCALAPPDATA\Microsoft\WindowsApps\essh.exe" -Target "$env:LOCALAPPDATA\essh\essh.exe"
```

### From source

```bash
go install github.com/chenyhd/essh@latest
```

Or build manually:

```bash
git clone https://github.com/chenyhd/essh.git
cd essh
go build -o essh .
```

## Usage

### 1. Initialize

```bash
essh init
```

Prompts for:
- **Storage directory** — where to save `essh-storage.json` (default: `~/.essh/`)
- **Encryption password** — used to encrypt/decrypt all SSH passwords (enter twice to confirm)

### 2. Add a server

```bash
essh add <name> <user@host[:port]>
```

Example:

```bash
essh add prod-web root@192.168.1.100
essh add dev-db admin@10.0.0.5:2222
```

Prompts for encryption password (to verify), then the SSH password for that server.

### 3. List servers

```bash
essh list
```

Shows all saved servers:

```
NAME      ADDRESS
prod-web  root@192.168.1.100:22
dev-db    admin@10.0.0.5:2222
```

### 4. Remove a server

```bash
essh remove prod-web
```

### 5. Rename a server

```bash
essh rename prod-web production
```

### 6. Edit a server

```bash
essh edit prod-web
```

Prompts for encryption password, then lets you change user, host, port, and SSH password. Leave a field empty to keep its current value.

### 7. Change encryption password

```bash
essh passwd
```

Prompts for the current password, then a new password (with confirmation). Re-encrypts all saved SSH passwords with the new key.

### 8. Check storage version

```bash
essh version
```

Shows the storage file path and version number. The version increments on every change, useful for checking if the file has been updated (e.g. after syncing via Git).

### 9. Copy files (SCP)

```bash
# Download a file from remote server
essh scp prod-web:/etc/hostname ./hostname.txt

# Upload a file to remote server
essh scp ./hostname.txt prod-web:/tmp/hostname.txt
```

Uses the same saved credentials. Direction is determined by which argument contains `<name>:`.

### 10. Connect

```bash
essh prod-web
```

Prompts for encryption password, decrypts the SSH password, and opens an interactive shell.

Prefix matching is supported — `essh p` will connect to `prod-web` if it's the only server starting with "p". If multiple servers match, they are listed for you to be more specific.

### 11. Interactive selection

```bash
essh
```

Running `essh` with no arguments opens an interactive server selector. Use arrow keys or `j`/`k` to move, `Enter` to select, `q` or `Ctrl+C` to cancel. The last connected server is pre-selected.

### 12. Reconnect last server

```bash
essh -
```

Quickly reconnect to the last server you connected to.

## Tab Completion

```bash
# Zsh (add to ~/.zshrc)
eval "$(essh completion zsh)"

# Bash (add to ~/.bashrc)
eval "$(essh completion bash)"
```

Enables tab completion for commands and server names.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ESSH_PASSWORD` | Skip encryption password prompt. Useful for scripting or frequent use |

## Session Password Cache

After a successful `connect`, `add`, `edit`, or `scp`, the encryption password is cached for **30 minutes**. Subsequent commands within that window will not prompt for the password again.

For security, `remove` and `passwd` always require you to enter the password regardless of cache.

## Keyfile (Two-Factor Protection)

During `essh init`, a **keyfile** is generated by default for two-factor protection. The keyfile is a 32-byte random file that is mixed into key derivation — both the encryption password **and** the keyfile are required to decrypt stored passwords.

```bash
essh init
# Storage directory (leave empty for ~/.essh):
# Encryption password: ****
# Confirm password: ****
# Keyfile path (enter "none" to skip) [~/.essh/essh.key]:
# Generated keyfile at /home/user/.essh/essh.key
```

- Press Enter to use the default path (`~/.essh/essh.key`)
- Enter a custom path (e.g. `/mnt/usb/essh.key`) to store it on a USB drive
- Enter `none` to skip keyfile setup (password-only mode)

The keyfile path is saved in `~/.essh/config.json`. If someone obtains your `essh-storage.json` without the keyfile, brute-force attacks are infeasible regardless of password strength.

**Important:** Back up your keyfile — if lost, stored passwords cannot be recovered.

## Multi-Device Sync via Git

You can sync `essh-storage.json` across devices using Git. During `essh init`, a `.gitignore` is created in the storage directory to exclude `*.key` files, so the keyfile stays local while the encrypted storage file can be pushed to a private repo.

```bash
cd ~/.essh
git init
git add essh-storage.json .gitignore config.json
git remote add origin <your-private-repo>
git push
```

On another device, clone the repo to `~/.essh/`, then run `essh init` with the same password and copy your keyfile over. Use `essh version` to check if the storage has been updated after pulling.

## Portability

The storage file (`essh-storage.json`) is self-contained. Copy it to another machine, run `essh init` pointing to its directory, and use the same encryption password to connect. If using a keyfile, copy the keyfile as well and ensure the config points to its new location.
