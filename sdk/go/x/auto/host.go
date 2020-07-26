package auto

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/sdk/v2/go/pulumi"
)

func (s *stack) host(isPreview bool) (string, string, error) {
	var stdout bytes.Buffer
	var errBuff bytes.Buffer
	args := []string{"host"}
	if isPreview {
		args = append(args, "preview")
	}
	cmd := exec.Command("pulumi", args...)
	cmd.Dir = s.SourcePath
	cmd.Stdout = &stdout
	stderr, _ := cmd.StderrPipe()
	cmd.Start()
	scanner := bufio.NewScanner(stderr)
	scanner.Split(bufio.ScanLines)

	addrChan := make(chan string)
	failChan := make(chan bool)
	go func() {
		success := false
		for scanner.Scan() {
			m := scanner.Text()
			errBuff.WriteString(m)
			if strings.HasPrefix(m, "127.0.0.1:") {
				success = true
				addrChan <- m
			}
		}
		if !success {
			failChan <- true
		}
	}()
	var monitorAddr string
	select {
	case <-failChan:
		return stdout.String(), errBuff.String(), errors.New("failed to launch host")
	case monitorAddr = <-addrChan:
	}

	os.Setenv(pulumi.EnvMonitor, monitorAddr)
	os.Setenv(pulumi.EnvProject, s.ProjectName)
	os.Setenv(pulumi.EnvStack, s.Name)
	cfg, err := s.rawConfig()
	if err != nil {
		return stdout.String(), errBuff.String(), errors.Wrap(err, "failed to serialize config for inline program")
	}
	cfgStr, err := json.Marshal(cfg)
	if err != nil {
		return stdout.String(), errBuff.String(), errors.Wrap(err, "unable to marshal config")
	}
	os.Setenv(pulumi.EnvConfig, string(cfgStr))
	err = pulumi.RunErr(s.InlineSource)
	if err != nil {
		cmd.Process.Signal(os.Interrupt)
		err = cmd.Process.Kill()
		if err != nil {
			return stdout.String(), errBuff.String(), errors.Wrap(err, "failed to run inline program and shutdown gracefully")
		}
		return stdout.String(), errBuff.String(), errors.Wrap(err, "error running inline pulumi program")
	}

	err = cmd.Process.Signal(os.Interrupt)
	if err != nil {
		return stdout.String(), errBuff.String(), errors.Wrap(err, "failed to shutdown host gracefully")
	}
	// TODO does this wait need to happen as a part of shutdown routine before returning error?
	cmd.Wait()

	return stdout.String(), errBuff.String(), nil
}
