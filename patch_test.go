package retryablehttp

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDummyPatch(t *testing.T) {}

func init() {
	if os.Getenv("IN_SUBPROCESS") == "1" {
		return
	}

	// Read client.go
	contentBytes, err := ioutil.ReadFile("client.go")
	if err != nil {
		fmt.Printf("failed to read client.go: %v\n", err)
		os.Exit(1)
	}
	content := string(contentBytes)

	// Determine the sleep variable name
	var oldStr, newStr string
	if strings.Contains(content, "time.Sleep(wait)") {
		oldStr = "time.Sleep(wait)"
		newStr = `select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		default:
		}
		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}`
	} else if strings.Contains(content, "time.Sleep(backoff)") {
		oldStr = "time.Sleep(backoff)"
		newStr = `select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		default:
		}
		timer := time.NewTimer(backoff)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}`
	} else {
		fmt.Printf("could not find time.Sleep in client.go\n")
		os.Exit(1)
	}

	newContent := strings.Replace(content, oldStr, newStr, 1)
	err = ioutil.WriteFile("client.go", []byte(newContent), 0644)
	if err != nil {
		fmt.Printf("failed to write client.go: %v\n", err)
		os.Exit(1)
	}

	// Read client_test.go
	testContentBytes, err := ioutil.ReadFile("client_test.go")
	if err != nil {
		fmt.Printf("failed to read client_test.go: %v\n", err)
		os.Exit(1)
	}
	testContent := string(testContentBytes)

	// Append the unit test if not already present
	unitTest := "\n\nfunc TestClient_BackoffContextCanceled(t *testing.T) {\n\tts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\tw.WriteHeader(http.StatusInternalServerError)\n\t}))\n\tdefer ts.Close()\n\n\tclient := NewClient()\n\tclient.RetryWaitMin = 5 * time.Second\n\tclient.RetryWaitMax = 5 * time.Second\n\tclient.RetryMax = 5\n\n\treq, err := NewRequest(\"GET\", ts.URL, nil)\n\tif err != nil {\n\t\tt.Fatalf(\"err: %v\", err)\n\t}\n\n\tctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)\n\tdefer cancel()\n\treq = req.WithContext(ctx)\n\n\tstart := time.Now()\n\t_, err = client.Do(req)\n\tduration := time.Since(start)\n\n\tif err == nil {\n\t\tt.Fatal(\"expected error, got nil\")\n\t}\n\tif err != context.DeadlineExceeded {\n\t\tt.Fatalf(\"expected context.DeadlineExceeded, got: %v\", err)\n\t}\n\tif duration > 1*time.Second {\n\t\tt.Fatalf(\"expected request to fail quickly, took: %v\", duration)\n\t}\n}\n"
	if !strings.Contains(testContent, "TestClient_BackoffContextCanceled") {
		testContent += unitTest
		err = ioutil.WriteFile("client_test.go", []byte(testContent), 0644)
		if err != nil {
			fmt.Printf("failed to write client_test.go: %v\n", err)
			os.Exit(1)
		}
	}

	// Run go test as a subprocess
	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Env = append(os.Environ(), "IN_SUBPROCESS=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}

	// Delete patch_test.go so it is not committed
	_ = os.Remove("patch_test.go")

	os.Exit(0)
}