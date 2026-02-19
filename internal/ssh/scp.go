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
		return fmt.Errorf("%s is a directory", localPath)
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
