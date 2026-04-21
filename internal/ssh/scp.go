package ssh

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Upload sends a local file to a remote path via the SCP protocol.
func Upload(client *ssh.Client, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening local file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory (use -r to upload recursively)", localPath)
	}

	fmt.Printf("Uploading %s (%s)...", filepath.Base(localPath), formatSize(info.Size()))

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("getting stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %w", err)
	}

	if err := session.Start("scp -t " + remotePath); err != nil {
		return fmt.Errorf("starting scp: %w", err)
	}

	// Read initial OK
	if err := readAck(stdout); err != nil {
		return fmt.Errorf("initial ack: %w", err)
	}

	// Send file header: C<mode> <size> <filename>
	header := fmt.Sprintf("C0644 %d %s\n", info.Size(), filepath.Base(localPath))
	if _, err := io.WriteString(stdin, header); err != nil {
		return fmt.Errorf("sending header: %w", err)
	}

	if err := readAck(stdout); err != nil {
		return fmt.Errorf("header ack: %w", err)
	}

	// Send file content
	if _, err := io.Copy(stdin, f); err != nil {
		return fmt.Errorf("sending file: %w", err)
	}

	// Send completion byte
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("sending completion: %w", err)
	}

	if err := readAck(stdout); err != nil {
		return fmt.Errorf("final ack: %w", err)
	}

	stdin.Close()
	if err := session.Wait(); err != nil {
		return fmt.Errorf("scp session: %w", err)
	}

	fmt.Println("done")
	return nil
}

// UploadRecursive sends a local file or directory tree to a remote path.
func UploadRecursive(client *ssh.Client, localPath, remotePath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat local path: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("getting stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %w", err)
	}

	if err := session.Start("scp -rt " + remotePath); err != nil {
		return fmt.Errorf("starting scp: %w", err)
	}

	if err := readAck(stdout); err != nil {
		return fmt.Errorf("initial ack: %w", err)
	}

	if info.IsDir() {
		fmt.Printf("Uploading directory %s...\n", localPath)
		if err := sendDir(stdin, stdout, localPath); err != nil {
			return err
		}
	} else {
		fmt.Printf("Uploading %s (%s)...\n", filepath.Base(localPath), formatSize(info.Size()))
		if err := sendFile(stdin, stdout, localPath, info); err != nil {
			return err
		}
	}

	stdin.Close()
	if err := session.Wait(); err != nil {
		return fmt.Errorf("scp session: %w", err)
	}

	fmt.Println("done")
	return nil
}

func sendFile(stdin io.Writer, stdout io.Reader, path string, info os.FileInfo) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	mode := info.Mode().Perm()
	header := fmt.Sprintf("C%04o %d %s\n", mode, info.Size(), filepath.Base(path))
	if _, err := io.WriteString(stdin, header); err != nil {
		return fmt.Errorf("sending header for %s: %w", path, err)
	}
	if err := readAck(stdout); err != nil {
		return fmt.Errorf("header ack for %s: %w", path, err)
	}
	if _, err := io.Copy(stdin, f); err != nil {
		return fmt.Errorf("sending %s: %w", path, err)
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("sending completion for %s: %w", path, err)
	}
	if err := readAck(stdout); err != nil {
		return fmt.Errorf("final ack for %s: %w", path, err)
	}
	fmt.Printf("  %s (%s)\n", path, formatSize(info.Size()))
	return nil
}

func sendDir(stdin io.Writer, stdout io.Reader, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	mode := info.Mode().Perm()
	header := fmt.Sprintf("D%04o 0 %s\n", mode, filepath.Base(path))
	if _, err := io.WriteString(stdin, header); err != nil {
		return fmt.Errorf("sending dir header for %s: %w", path, err)
	}
	if err := readAck(stdout); err != nil {
		return fmt.Errorf("dir header ack for %s: %w", path, err)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("reading dir %s: %w", path, err)
	}

	for _, e := range entries {
		full := filepath.Join(path, e.Name())
		if e.IsDir() {
			if err := sendDir(stdin, stdout, full); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() {
			fmt.Printf("  skipping non-regular: %s\n", full)
			continue
		}
		fi, err := e.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", full, err)
		}
		if err := sendFile(stdin, stdout, full, fi); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(stdin, "E\n"); err != nil {
		return fmt.Errorf("sending E for %s: %w", path, err)
	}
	if err := readAck(stdout); err != nil {
		return fmt.Errorf("E ack for %s: %w", path, err)
	}
	return nil
}

// Download retrieves a remote file to a local path via the SCP protocol.
func Download(client *ssh.Client, remotePath, localPath string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("getting stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %w", err)
	}

	if err := session.Start("scp -f " + remotePath); err != nil {
		return fmt.Errorf("starting scp: %w", err)
	}

	// Send initial OK to request file
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("sending initial ack: %w", err)
	}

	// Read file header: C<mode> <size> <filename>
	header, err := readLine(stdout)
	if err != nil {
		return fmt.Errorf("reading header: %w", err)
	}

	if len(header) == 0 || header[0] != 'C' {
		return fmt.Errorf("unexpected scp header: %q", header)
	}

	size, filename, err := parseHeader(header)
	if err != nil {
		return err
	}

	// Determine final local path — if localPath is a directory, append the filename
	fi, err := os.Stat(localPath)
	if err == nil && fi.IsDir() {
		localPath = filepath.Join(localPath, filename)
	}

	fmt.Printf("Downloading %s (%s)...", filename, formatSize(size))

	// Send OK to acknowledge header
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("sending header ack: %w", err)
	}

	// Read file content
	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("creating local file: %w", err)
	}
	defer f.Close()

	if _, err := io.CopyN(f, stdout, size); err != nil {
		return fmt.Errorf("receiving file: %w", err)
	}

	// Read trailing zero byte
	buf := make([]byte, 1)
	if _, err := io.ReadFull(stdout, buf); err != nil {
		return fmt.Errorf("reading trailing byte: %w", err)
	}

	// Send final OK
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("sending final ack: %w", err)
	}

	stdin.Close()
	if err := session.Wait(); err != nil {
		return fmt.Errorf("scp session: %w", err)
	}

	fmt.Println("done")
	return nil
}

// DownloadRecursive retrieves a remote file or directory tree to a local path.
func DownloadRecursive(client *ssh.Client, remotePath, localPath string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("getting stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %w", err)
	}

	if err := session.Start("scp -rf " + remotePath); err != nil {
		return fmt.Errorf("starting scp: %w", err)
	}

	localIsDir := false
	if fi, err := os.Stat(localPath); err == nil && fi.IsDir() {
		localIsDir = true
	}

	var stack []string

	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("initial ack: %w", err)
	}

	for {
		line, err := readLine(stdout)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("reading directive: %w", err)
		}
		if len(line) == 0 {
			continue
		}

		switch line[0] {
		case 0x01, 0x02:
			return fmt.Errorf("scp remote error: %s", strings.TrimSpace(line[1:]))
		case 'T':
			// timestamps — ack and ignore (not requested, but tolerate)
			if _, err := stdin.Write([]byte{0}); err != nil {
				return fmt.Errorf("ack T: %w", err)
			}
		case 'C':
			mode, size, name, err := parseCDLine(line)
			if err != nil {
				return err
			}
			dst := resolveDownloadTarget(stack, localPath, localIsDir, name)
			if _, err := stdin.Write([]byte{0}); err != nil {
				return fmt.Errorf("header ack: %w", err)
			}
			if err := receiveFile(stdin, stdout, dst, mode, size); err != nil {
				return err
			}
		case 'D':
			mode, _, name, err := parseCDLine(line)
			if err != nil {
				return err
			}
			dst := resolveDownloadTarget(stack, localPath, localIsDir, name)
			if err := os.MkdirAll(dst, mode); err != nil {
				return fmt.Errorf("creating dir %s: %w", dst, err)
			}
			stack = append(stack, dst)
			if _, err := stdin.Write([]byte{0}); err != nil {
				return fmt.Errorf("dir ack: %w", err)
			}
		case 'E':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if _, err := stdin.Write([]byte{0}); err != nil {
				return fmt.Errorf("E ack: %w", err)
			}
		default:
			return fmt.Errorf("unexpected scp directive: %q", line)
		}
	}

	stdin.Close()
	if err := session.Wait(); err != nil {
		return fmt.Errorf("scp session: %w", err)
	}

	fmt.Println("done")
	return nil
}

func resolveDownloadTarget(stack []string, localPath string, localIsDir bool, name string) string {
	if len(stack) > 0 {
		return filepath.Join(stack[len(stack)-1], name)
	}
	if localIsDir {
		return filepath.Join(localPath, name)
	}
	return localPath
}

func receiveFile(stdin io.Writer, stdout io.Reader, dst string, mode os.FileMode, size int64) error {
	fmt.Printf("  %s (%s)\n", dst, formatSize(size))
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	if _, err := io.CopyN(f, stdout, size); err != nil {
		f.Close()
		return fmt.Errorf("receiving %s: %w", dst, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", dst, err)
	}
	buf := make([]byte, 1)
	if _, err := io.ReadFull(stdout, buf); err != nil {
		return fmt.Errorf("reading trailing byte: %w", err)
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("final ack: %w", err)
	}
	return nil
}

// parseCDLine parses a C or D scp directive: "C0644 12345 name" or "D0755 0 name".
func parseCDLine(line string) (mode os.FileMode, size int64, name string, err error) {
	if len(line) < 2 {
		return 0, 0, "", fmt.Errorf("scp line too short: %q", line)
	}
	parts := strings.SplitN(line[1:], " ", 3)
	if len(parts) != 3 {
		return 0, 0, "", fmt.Errorf("invalid scp line: %q", line)
	}
	m, err := strconv.ParseUint(parts[0], 8, 32)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid mode in %q: %w", line, err)
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid size in %q: %w", line, err)
	}
	return os.FileMode(m), n, parts[2], nil
}

// readAck reads a single SCP acknowledgment byte. 0 = OK, 1 = warning, 2 = error.
func readAck(r io.Reader) error {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(r, buf); err != nil {
		return fmt.Errorf("reading ack: %w", err)
	}
	if buf[0] != 0 {
		// Read the error message
		msg, _ := readLine(r)
		return fmt.Errorf("scp error (code %d): %s", buf[0], msg)
	}
	return nil
}

// readLine reads until a newline and returns the line without the newline.
func readLine(r io.Reader) (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return string(line), err
		}
		if buf[0] == '\n' {
			return string(line), nil
		}
		line = append(line, buf[0])
	}
}

// parseHeader parses an SCP file header like "C0644 12345 file.txt".
func parseHeader(header string) (size int64, filename string, err error) {
	parts := strings.SplitN(header, " ", 3)
	if len(parts) != 3 {
		return 0, "", fmt.Errorf("invalid scp header: %q", header)
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid size in header: %w", err)
	}
	return n, parts[2], nil
}

// formatSize returns a human-readable file size.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
