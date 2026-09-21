package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type ModelFile struct {
	Name          string
	URL           string
	ExpectedBytes int64
	ExpectedSHA   string
}

var files = []ModelFile{
	{
		Name:          "Qwen3.8-Flash-Next-UD-IQ3_XXS-00001-of-00003.gguf",
		URL:           "https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/resolve/main/UD-IQ3_XXS/Qwen3.8-Flash-Next-UD-IQ3_XXS-00001-of-00003.gguf",
		ExpectedBytes: 10946624,
		ExpectedSHA:   "268f81fdedf3149a538f252308927a4d5d1f6e062c178568a51e3b519744f8a8",
	},
	{
		Name:          "Qwen3.8-Flash-Next-UD-IQ3_XXS-00002-of-00003.gguf",
		URL:           "https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/resolve/main/UD-IQ3_XXS/Qwen3.8-Flash-Next-UD-IQ3_XXS-00002-of-00003.gguf",
		ExpectedBytes: 49567921344,
		ExpectedSHA:   "cfe600b236b88c7fad1613a5ca5e83b9f2beb63cbd44c32b2be50a44747c695f",
	},
	{
		Name:          "Qwen3.8-Flash-Next-UD-IQ3_XXS-00003-of-00003.gguf",
		URL:           "https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/resolve/main/UD-IQ3_XXS/Qwen3.8-Flash-Next-UD-IQ3_XXS-00003-of-00003.gguf",
		ExpectedBytes: 32382955968,
		ExpectedSHA:   "f1912ba34c79427d2295a58dcb2b732b5931af5bef7a373c60557a57d9ee7250",
	},
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func downloadFile(client *http.Client, destDir string, mf ModelFile, verifySHA bool) error {
	finalPath := filepath.Join(destDir, mf.Name)
	partPath := finalPath + ".part"

	if st, err := os.Stat(finalPath); err == nil {
		if st.Size() == mf.ExpectedBytes {
			fmt.Printf("✓ %s already downloaded and complete (%s)\n", mf.Name, formatBytes(st.Size()))
			if verifySHA && mf.ExpectedSHA != "" {
				fmt.Printf("  Verifying SHA256 checksum...\n")
				if err := verifyFileSHA(finalPath, mf.ExpectedSHA); err != nil {
					return fmt.Errorf("SHA256 verification failed: %w", err)
				}
				fmt.Printf("  ✓ SHA256 verified successfully\n")
			}
			return nil
		}
	}

	var startOffset int64 = 0
	if st, err := os.Stat(partPath); err == nil {
		startOffset = st.Size()
		if startOffset > mf.ExpectedBytes {
			fmt.Printf("! Partial file exceeds expected size (%d > %d), restarting\n", startOffset, mf.ExpectedBytes)
			_ = os.Remove(partPath)
			startOffset = 0
		} else if startOffset > 0 {
			fmt.Printf("→ Resuming %s from offset %s (%d bytes, %.1f%%)\n",
				mf.Name, formatBytes(startOffset), startOffset, float64(startOffset)*100/float64(mf.ExpectedBytes))
		}
	}

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("cannot open file %s: %w", partPath, err)
	}
	defer f.Close()

	if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
		return fmt.Errorf("seek failed: %w", err)
	}

	req, err := http.NewRequest("GET", mf.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Cuda38flash39-ModelDownloader/1.0")
	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer resp.Body.Close()

	if startOffset > 0 && resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusOK {
			fmt.Printf("! Server did not honor Range request, restarting from offset 0\n")
			_ = f.Truncate(0)
			_, _ = f.Seek(0, io.SeekStart)
			startOffset = 0
		} else {
			return fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
		}
	} else if startOffset == 0 && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	fmt.Printf("↓ Downloading %s (%s total)...\n", mf.Name, formatBytes(mf.ExpectedBytes))

	buf := make([]byte, 1024*1024) // 1MB buffer
	currentOffset := startOffset
	lastReport := time.Now()
	lastBytes := currentOffset
	startTime := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write failed: %w", writeErr)
			}
			currentOffset += int64(n)

			if time.Since(lastReport) >= 2*time.Second {
				elapsedSec := time.Since(lastReport).Seconds()
				bytesDiff := currentOffset - lastBytes
				speed := float64(bytesDiff) / elapsedSec
				pct := float64(currentOffset) * 100 / float64(mf.ExpectedBytes)

				var etaStr string
				if speed > 0 {
					remBytes := mf.ExpectedBytes - currentOffset
					etaSec := float64(remBytes) / speed
					etaDur := time.Duration(etaSec) * time.Second
					etaStr = fmt.Sprintf("ETA: %s", etaDur.Round(time.Second))
				} else {
					etaStr = "ETA: --"
				}

				fmt.Printf("\r  [%6.2f%%] %s / %s | %s/s | %s   ",
					pct, formatBytes(currentOffset), formatBytes(mf.ExpectedBytes), formatBytes(int64(speed)), etaStr)
				lastReport = time.Now()
				lastBytes = currentOffset
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return fmt.Errorf("stream read error: %w", readErr)
		}
	}

	_ = f.Sync()
	f.Close()

	overallSec := time.Since(startTime).Seconds()
	overallSpeed := float64(currentOffset-startOffset) / overallSec
	fmt.Printf("\n✓ Download finished: %s (%s downloaded at %s/s)\n",
		mf.Name, formatBytes(currentOffset), formatBytes(int64(overallSpeed)))

	if currentOffset != mf.ExpectedBytes {
		return fmt.Errorf("download size mismatch: got %d bytes, expected %d", currentOffset, mf.ExpectedBytes)
	}

	if verifySHA && mf.ExpectedSHA != "" {
		fmt.Printf("  Verifying SHA256 checksum...\n")
		if err := verifyFileSHA(partPath, mf.ExpectedSHA); err != nil {
			return fmt.Errorf("SHA256 checksum verification failed: %w", err)
		}
		fmt.Printf("  ✓ SHA256 matches %s\n", mf.ExpectedSHA[:12]+"...")
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		return fmt.Errorf("atomic rename failed: %w", err)
	}
	fmt.Printf("✓ Saved to %s\n", finalPath)
	return nil
}

func verifyFileSHA(p string, expectedHex string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, 4*1024*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return err
	}

	sumHex := hex.EncodeToString(h.Sum(nil))
	if sumHex != expectedHex {
		return fmt.Errorf("hash mismatch: got %s, expected %s", sumHex, expectedHex)
	}
	return nil
}

func main() {
	destDir := flag.String("dest", "/home/funboy/models/gguf/qwen3.8-flash-next-unsloth-iq3-xxs/UD-IQ3_XXS", "Destination directory")
	partOnly := flag.Int("part", 0, "Download only specific part (1, 2, or 3). 0 downloads all parts")
	verifySHA := flag.Bool("verify-sha", false, "Verify full SHA256 after download")
	flag.Parse()

	if err := os.MkdirAll(*destDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating destination dir: %v\n", err)
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n! Received interrupt signal. Partial files preserved. Resume by running the tool again.")
		os.Exit(130)
	}()

	client := &http.Client{
		Timeout: 0, // No global timeout for long transfers
	}

	fmt.Printf("================================================================\n")
	fmt.Printf(" Cuda38flash39: Qwen 3.8 Flash Next UD-IQ3_XXS Resumable Downloader\n")
	fmt.Printf(" Destination: %s\n", *destDir)
	fmt.Printf("================================================================\n")

	for i, mf := range files {
		partNum := i + 1
		if *partOnly != 0 && *partOnly != partNum {
			continue
		}

		fmt.Printf("\n--> Processing Part %d/3: %s\n", partNum, mf.Name)
		if err := downloadFile(client, *destDir, mf, *verifySHA); err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading part %d: %v\n", partNum, err)
			os.Exit(1)
		}
	}

	fmt.Printf("\n✓ All selected model parts successfully downloaded and ready for Cuda38flash39!\n")
}
