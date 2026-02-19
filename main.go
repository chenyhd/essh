package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"essh/internal/config"
	"essh/internal/crypto"
	"essh/internal/prompt"
	"essh/internal/ssh"
	"essh/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit()
	case "add":
		err = cmdAdd()
	case "list":
		err = cmdList()
	case "remove":
		err = cmdRemove()
	case "rename":
		err = cmdRename()
	case "edit":
		err = cmdEdit()
	case "passwd":
		err = cmdPasswd()
	case "scp":
		err = cmdScp()
	case "completion":
		err = cmdCompletion()
	case "--names":
		err = cmdNames()
	case "help", "--help", "-h":
		printUsage()
	default:
		err = cmdConnect(os.Args[1])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`essh - encrypted SSH client

Usage:
  essh init                    Initialize storage with encryption password
  essh add <name> <user@host[:port]>  Add a server
  essh list                    List saved servers
  essh remove <name>           Remove a saved server
  essh rename <old> <new>      Rename a saved server
  essh edit <name>             Edit a saved server
  essh passwd                  Change encryption password
  essh scp <src> <dst>         Copy files (use <name>:/path for remote)
  essh completion              Output shell completion script (bash/zsh)
  essh <name>                  Connect to a saved server

Environment:
  ESSH_PASSWORD                Skip encryption password prompt`)
}

func cmdInit() error {
	dir, err := prompt.ReadLine("Storage directory (leave empty for ~/.essh): ")
	if err != nil {
		return err
	}
	if dir == "" {
		d, err := config.Dir()
		if err != nil {
			return err
		}
		dir = d
	}

	// Expand ~ if present
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, dir[2:])
	}

	encPassword, err := prompt.ReadPasswordConfirm("Encryption password: ", "Confirm password: ")
	if err != nil {
		return err
	}
	if encPassword == "" {
		return fmt.Errorf("password cannot be empty")
	}

	storagePath := filepath.Join(dir, "essh-storage.json")

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	if err := storage.Init(storagePath, encPassword); err != nil {
		return err
	}

	if err := config.Save(&config.Config{StoragePath: storagePath}); err != nil {
		return err
	}

	fmt.Printf("Initialized essh storage at %s\n", storagePath)
	return nil
}

func cmdAdd() error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: essh add <name> <user@host[:port]>")
	}
	name := os.Args[2]
	target := os.Args[3]

	user, host, port, err := parseTarget(target)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	encPassword, err := prompt.ReadPassword("Encryption password: ")
	if err != nil {
		return err
	}

	key, err := store.VerifyPassword(encPassword)
	if err != nil {
		return err
	}

	sshPassword, err := prompt.ReadPassword("SSH password for " + user + "@" + host + ": ")
	if err != nil {
		return err
	}

	encrypted, err := crypto.Encrypt(key, sshPassword)
	if err != nil {
		return err
	}

	srv := storage.Server{
		Name:              name,
		User:              user,
		Host:              host,
		Port:              port,
		EncryptedPassword: encrypted,
	}

	if err := store.AddServer(srv); err != nil {
		return err
	}

	if err := storage.Save(cfg.StoragePath, store); err != nil {
		return err
	}

	fmt.Printf("Added server %q (%s@%s:%d)\n", name, user, host, port)
	return nil
}

func cmdList() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	if len(store.Servers) == 0 {
		fmt.Println("No servers saved. Use 'essh add' to add one.")
		return nil
	}

	// Calculate column widths
	nameW := 4
	addrW := 7
	for _, s := range store.Servers {
		if len(s.Name) > nameW {
			nameW = len(s.Name)
		}
		addr := fmt.Sprintf("%s@%s:%d", s.User, s.Host, s.Port)
		if len(addr) > addrW {
			addrW = len(addr)
		}
	}

	fmt.Printf("%-*s  %s\n", nameW, "NAME", "ADDRESS")
	for _, s := range store.Servers {
		fmt.Printf("%-*s  %s@%s:%d\n", nameW, s.Name, s.User, s.Host, s.Port)
	}
	return nil
}

func cmdRemove() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: essh remove <name>")
	}
	name := os.Args[2]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	if store.FindServer(name) == nil {
		return fmt.Errorf("server %q not found", name)
	}

	encPassword, err := prompt.ReadPassword("Encryption password: ")
	if err != nil {
		return err
	}

	if _, err := store.VerifyPassword(encPassword); err != nil {
		return err
	}

	ok, err := prompt.Confirm(fmt.Sprintf("Remove server %q? [y/N] ", name))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := store.RemoveServer(name); err != nil {
		return err
	}

	if err := storage.Save(cfg.StoragePath, store); err != nil {
		return err
	}

	fmt.Printf("Removed server %q\n", name)
	return nil
}

func cmdRename() error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: essh rename <old> <new>")
	}
	oldName := os.Args[2]
	newName := os.Args[3]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	if err := store.RenameServer(oldName, newName); err != nil {
		return err
	}

	if err := storage.Save(cfg.StoragePath, store); err != nil {
		return err
	}

	fmt.Printf("Renamed %q -> %q\n", oldName, newName)
	return nil
}

func cmdEdit() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: essh edit <name>")
	}
	name := os.Args[2]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	srv := store.FindServer(name)
	if srv == nil {
		return fmt.Errorf("server %q not found", name)
	}

	encPassword, err := prompt.ReadPassword("Encryption password: ")
	if err != nil {
		return err
	}

	key, err := store.VerifyPassword(encPassword)
	if err != nil {
		return err
	}

	fmt.Printf("Editing %q (leave empty to keep current value)\n", name)

	newUser, err := prompt.ReadLine(fmt.Sprintf("User [%s]: ", srv.User))
	if err != nil {
		return err
	}
	if newUser != "" {
		srv.User = newUser
	}

	newHost, err := prompt.ReadLine(fmt.Sprintf("Host [%s]: ", srv.Host))
	if err != nil {
		return err
	}
	if newHost != "" {
		srv.Host = newHost
	}

	newPort, err := prompt.ReadLine(fmt.Sprintf("Port [%d]: ", srv.Port))
	if err != nil {
		return err
	}
	if newPort != "" {
		p, err := strconv.Atoi(newPort)
		if err != nil {
			return fmt.Errorf("invalid port: %s", newPort)
		}
		srv.Port = p
	}

	newSSHPw, err := prompt.ReadPassword("New SSH password (leave empty to keep): ")
	if err != nil {
		return err
	}
	if newSSHPw != "" {
		encrypted, err := crypto.Encrypt(key, newSSHPw)
		if err != nil {
			return err
		}
		srv.EncryptedPassword = encrypted
	}

	if err := storage.Save(cfg.StoragePath, store); err != nil {
		return err
	}

	fmt.Printf("Updated server %q\n", name)
	return nil
}

func cmdPasswd() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	oldPassword, err := prompt.ReadPassword("Current encryption password: ")
	if err != nil {
		return err
	}

	oldKey, err := store.VerifyPassword(oldPassword)
	if err != nil {
		return err
	}

	newPassword, err := prompt.ReadPasswordConfirm("New encryption password: ", "Confirm new password: ")
	if err != nil {
		return err
	}
	if newPassword == "" {
		return fmt.Errorf("password cannot be empty")
	}

	newSalt, err := crypto.GenerateSalt()
	if err != nil {
		return err
	}

	newKey := crypto.DeriveKey(newPassword, newSalt)

	newVerification, err := crypto.Encrypt(newKey, crypto.VerifyStr)
	if err != nil {
		return err
	}

	if err := store.ReEncryptAll(oldKey, newKey, newSalt, newVerification); err != nil {
		return err
	}

	if err := storage.Save(cfg.StoragePath, store); err != nil {
		return err
	}

	fmt.Println("Encryption password changed successfully.")
	return nil
}

func cmdNames() error {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return nil
	}
	for _, s := range store.Servers {
		fmt.Println(s.Name)
	}
	return nil
}

func cmdCompletion() error {
	shell := "zsh"
	if len(os.Args) >= 3 {
		shell = os.Args[2]
	}
	switch shell {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	default:
		return fmt.Errorf("unsupported shell %q (use bash or zsh)", shell)
	}
	return nil
}

const bashCompletion = `_essh() {
    local cur commands
    cur="${COMP_WORDS[COMP_CWORD]}"
    commands="init add list remove rename edit passwd scp completion help"

    if [ "$COMP_CWORD" -eq 1 ]; then
        local names
        names=$(essh --names 2>/dev/null)
        COMPREPLY=($(compgen -W "$commands $names" -- "$cur"))
    elif [ "$COMP_CWORD" -eq 2 ]; then
        case "${COMP_WORDS[1]}" in
            remove|edit|rename)
                local names
                names=$(essh --names 2>/dev/null)
                COMPREPLY=($(compgen -W "$names" -- "$cur"))
                ;;
            scp)
                local names
                names=$(essh --names 2>/dev/null)
                local colon_names=""
                for n in $names; do colon_names="$colon_names $n:"; done
                COMPREPLY=($(compgen -W "$colon_names" -- "$cur"))
                compopt -o nospace
                ;;
        esac
    fi
}
complete -F _essh essh
`

const zshCompletion = `#compdef essh

_essh() {
    local -a commands names
    commands=(
        'init:Initialize storage with encryption password'
        'add:Add a server'
        'list:List saved servers'
        'remove:Remove a saved server'
        'rename:Rename a saved server'
        'edit:Edit a saved server'
        'passwd:Change encryption password'
        'scp:Copy files to/from a server'
        'completion:Output shell completion script'
        'help:Show help'
    )
    names=(${(f)"$(essh --names 2>/dev/null)"})

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        compadd -a names
    elif (( CURRENT == 3 )); then
        case "${words[2]}" in
            remove|edit|rename)
                compadd -a names
                ;;
            scp)
                local -a colon_names
                for n in $names; do colon_names+=("$n:"); done
                compadd -S '' -a colon_names
                ;;
        esac
    fi
}

_essh "$@"
`

func cmdScp() error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: essh scp <src> <dst>\n  Use <name>:/path for remote, e.g.:\n    essh scp prod-web:/etc/hostname ./hostname.txt\n    essh scp ./file.txt prod-web:/tmp/file.txt")
	}
	src := os.Args[2]
	dst := os.Args[3]

	// Determine direction: whichever arg contains "<name>:" is the remote side
	srcName, srcPath := splitScpArg(src)
	dstName, dstPath := splitScpArg(dst)

	var serverName, remotePath, localPath string
	var upload bool

	switch {
	case srcName != "" && dstName != "":
		return fmt.Errorf("both arguments cannot be remote — copy between two remote servers is not supported")
	case srcName != "":
		serverName, remotePath, localPath = srcName, srcPath, dst
		upload = false
	case dstName != "":
		serverName, remotePath, localPath = dstName, dstPath, src
		upload = true
	default:
		return fmt.Errorf("one argument must be remote (e.g. prod-web:/path)")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	srv := store.FindServer(serverName)
	if srv == nil {
		return fmt.Errorf("server %q not found — use 'essh list' to see saved servers", serverName)
	}

	encPassword, err := prompt.ReadPassword("Encryption password: ")
	if err != nil {
		return err
	}

	key, err := store.VerifyPassword(encPassword)
	if err != nil {
		return err
	}

	sshPassword, err := crypto.Decrypt(key, srv.EncryptedPassword)
	if err != nil {
		return fmt.Errorf("decrypting password: %w", err)
	}

	client, err := ssh.Dial(srv.Host, srv.Port, srv.User, sshPassword)
	if err != nil {
		return err
	}
	defer client.Close()

	if upload {
		return ssh.Upload(client, localPath, remotePath)
	}
	return ssh.Download(client, remotePath, localPath)
}

// splitScpArg splits "name:/path" into ("name", "/path").
// Returns ("", arg) if there is no colon prefix matching a server name pattern.
func splitScpArg(arg string) (name, path string) {
	// A colon preceded by path separators or starting with . or / is a local path
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return "", arg
	}
	idx := strings.Index(arg, ":")
	if idx < 1 {
		return "", arg
	}
	return arg[:idx], arg[idx+1:]
}

func cmdConnect(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("not initialized — run 'essh init' first")
	}

	store, err := storage.Load(cfg.StoragePath)
	if err != nil {
		return err
	}

	srv := store.FindServer(name)
	if srv == nil {
		return fmt.Errorf("server %q not found — use 'essh list' to see saved servers", name)
	}

	encPassword, err := prompt.ReadPassword("Encryption password: ")
	if err != nil {
		return err
	}

	key, err := store.VerifyPassword(encPassword)
	if err != nil {
		return err
	}

	sshPassword, err := crypto.Decrypt(key, srv.EncryptedPassword)
	if err != nil {
		return fmt.Errorf("decrypting password: %w", err)
	}

	fmt.Printf("Connecting to %s@%s:%d...\n", srv.User, srv.Host, srv.Port)
	return ssh.Connect(srv.Host, srv.Port, srv.User, sshPassword)
}

func parseTarget(target string) (user, host string, port int, err error) {
	parts := strings.SplitN(target, "@", 2)
	if len(parts) != 2 {
		return "", "", 0, fmt.Errorf("invalid target %q — expected user@host[:port]", target)
	}
	user = parts[0]
	hostPort := parts[1]

	port = 22
	if colonIdx := strings.LastIndex(hostPort, ":"); colonIdx != -1 {
		host = hostPort[:colonIdx]
		p, err := strconv.Atoi(hostPort[colonIdx+1:])
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid port in %q", target)
		}
		port = p
	} else {
		host = hostPort
	}

	if user == "" || host == "" {
		return "", "", 0, fmt.Errorf("invalid target %q — user and host cannot be empty", target)
	}
	return user, host, port, nil
}
