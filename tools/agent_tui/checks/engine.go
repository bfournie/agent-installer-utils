package checks

import (
	"net/http"
	"os/exec"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	CheckTypeReleaseImagePull     = "ReleaseImagePull"
	CheckTypeReleaseImageHostDNS  = "ReleaseImageHostDNS"
	CheckTypeReleaseImageHostPing = "ReleaseImageHostPing"
	CheckTypeReleaseImageHttp     = "ReleaseImageHttp"

	WORKFLOW_TYPE_INSTALL_INTERACTIVE_DISCONNECTED = "install-interactive-disconnected"
)

type Config struct {
	ReleaseImageURL string
	LogPath         string

	ReleaseImageHostname           string
	ReleaseImageSchemeHostnamePort string

	WorkflowType string
}

// ChecksEngine is the model part, and is composed by a number
// of different checks.
// Each Check has a type, frequency and evaluation loop.
// Different checks could have the same type

type CheckResult struct {
	Type    string
	Success bool
	Details string // In case of failure
}

type Check struct {
	Type string
	Freq time.Duration //Note: a ticker could be useful
	Run  func(c chan CheckResult, Freq time.Duration)
}

type Engine struct {
	checks  []*Check
	channel chan CheckResult
	logger  *logrus.Logger
}

type CheckFunction func(checkType string, config Config) ([]byte, error)

func createCheckResult(f CheckFunction, checkType string, config Config, l *logrus.Logger) CheckResult {
	output, err := f(checkType, config)
	result := CheckResult{
		Type:    checkType,
		Success: err == nil,
		Details: string(output),
	}
	if result.Success {
		l.Infof("%s check successful: %s", checkType, result.Details)
	} else {
		l.Warnf("%s check failed with error: %s", checkType, result.Details)
	}
	return result
}

type CheckFunctions map[string]CheckFunction

var defaultCheckFunctions = CheckFunctions{
	CheckTypeReleaseImagePull: func(checkType string, c Config) ([]byte, error) {
		return exec.Command("podman", "pull", c.ReleaseImageURL).CombinedOutput()
	},
	CheckTypeReleaseImageHostDNS: func(checkType string, c Config) ([]byte, error) {
		return exec.Command("nslookup", c.ReleaseImageHostname).CombinedOutput()
	},
	CheckTypeReleaseImageHostPing: func(checkType string, c Config) ([]byte, error) {
		return exec.Command("ping", "-c", "4", c.ReleaseImageHostname).CombinedOutput()
	},
	CheckTypeReleaseImageHttp: func(checkType string, c Config) ([]byte, error) {
		resp, err := http.Get(c.ReleaseImageSchemeHostnamePort)
		if err != nil {
			return []byte(err.Error()), err
		} else {
			// server replied with http response
			// as long as there is a response, the check
			// is a success.
			return []byte(resp.Status), err
		}
	},
}

func NewEngine(c chan CheckResult, config Config, logger *logrus.Logger, checkFuncs ...CheckFunctions) *Engine {
	checks := []*Check{}

	// The install-interactive-disconnected workflow has the release image
	// contents included in its live ISO. Checking release image
	// connectivity is not needed.
	if config.WorkflowType != WORKFLOW_TYPE_INSTALL_INTERACTIVE_DISCONNECTED {
		cf := defaultCheckFunctions
		if len(checkFuncs) > 0 {
			cf = checkFuncs[0]
		}

		// create checks
		for cType, cFunc := range cf {
			ct := cType
			cf := cFunc
			checks = append(checks, &Check{
				Type: ct,
				Freq: 5 * time.Second,
				Run: func(c chan CheckResult, freq time.Duration) {
					for {
						c <- createCheckResult(cf, ct, config, logger)
						time.Sleep(freq)
					}
				},
			})
		}
	}

	return &Engine{
		checks:  checks,
		channel: c,
		logger:  logger,
	}
}

func (e *Engine) Init() {
	for _, chk := range e.checks {
		go chk.Run(e.channel, chk.Freq)
	}
}

func (e *Engine) Size() int {
	return len(e.checks)
}
