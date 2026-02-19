# essh - Encrypted SSH Client

Store SSH server credentials with encrypted passwords, connect by server name.

## Install

### macOS (Homebrew)

```bash
brew install chenyhd/tap/essh
```

### Linux (deb)

```bash
# Download the latest .deb from GitHub Releases
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_amd64.deb
sudo dpkg -i essh_linux_amd64.deb
```

### Linux (rpm)

```bash
# Download the latest .rpm from GitHub Releases
curl -LO https://github.com/chenyhd/essh/releases/latest/download/essh_linux_amd64.rpm
sudo rpm -i essh_linux_amd64.rpm
```

### Windows

Download the `.zip` from [GitHub Releases](https://github.com/chenyhd/essh/releases) and add `essh.exe` to your PATH.

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
./essh init
```

Prompts for:
- **Storage directory** — where to save `essh-storage.json` (default: `~/.essh/`)
- **Encryption password** — used to encrypt/decrypt all SSH passwords (enter twice to confirm)

### 2. Add a server

```bash
./essh add <name> <user@host[:port]>
```

Example:

```bash
./essh add prod-web root@192.168.1.100
./essh add dev-db admin@10.0.0.5:2222
```

Prompts for encryption password (to verify), then the SSH password for that server.

### 3. List servers

```bash
./essh list
```

Shows all saved servers:

```
NAME      ADDRESS
prod-web  root@192.168.1.100:22
dev-db    admin@10.0.0.5:2222
```

### 4. Remove a server

```bash
./essh remove prod-web
```

### 5. Rename a server

```bash
./essh rename prod-web production
```

### 6. Edit a server

```bash
./essh edit prod-web
```

Prompts for encryption password, then lets you change user, host, port, and SSH password. Leave a field empty to keep its current value.

### 7. Change encryption password

```bash
./essh passwd
```

Prompts for the current password, then a new password (with confirmation). Re-encrypts all saved SSH passwords with the new key.

### 8. Copy files (SCP)

```bash
# Download a file from remote server
./essh scp prod-web:/etc/hostname ./hostname.txt

# Upload a file to remote server
./essh scp ./hostname.txt prod-web:/tmp/hostname.txt
```

Uses the same saved credentials. Direction is determined by which argument contains `<name>:`.

### 9. Connect

```bash
./essh prod-web
```

Prompts for encryption password, decrypts the SSH password, and opens an interactive shell.

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

## Portability

The storage file (`essh-storage.json`) is self-contained. Copy it to another machine, run `./essh init` pointing to its directory, and use the same encryption password to connect.
