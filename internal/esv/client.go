package esv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/deref/exo/internal/util/logging"
)

var AuthError = errors.New("auth error")

type UserDescription struct {
	Me struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"me"`
}

type EsvClient interface {
	Unauthenticate(ctx context.Context) error
	StartAuthFlow(ctx context.Context) (AuthResponse, error)
	GetWorkspaceSecrets(vaultURL string) (map[string]string, error)
	DescribeSelf(ctx context.Context, vaultURL string) (*UserDescription, error)
	SaveRefreshToken(ctx context.Context, refreshToken string) error
}

func NewEsvClient(tokenPath string) *esvClient {
	return &esvClient{
		tokenPath:  tokenPath,
		tokenMutex: &sync.Mutex{},
	}
}

type esvClient struct {
	tokenPath string

	// tokenMutex locks both the refresh token and access token.
	tokenMutex            *sync.Mutex
	refreshToken          string
	accessToken           string
	accessTokenExpiration time.Time
}

var _ EsvClient = &esvClient{}

type AuthResponse struct {
	UserCode string
	AuthURL  string
}

func (c *esvClient) Unauthenticate(ctx context.Context) error {
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()
	if err := os.Remove(c.tokenPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing token file: %w", err)
	}
	c.accessToken = ""
	c.refreshToken = ""
	c.accessTokenExpiration = time.Time{}
	return nil
}

func (c *esvClient) DescribeSelf(ctx context.Context, vaultURL string) (*UserDescription, error) {
	resp := &UserDescription{}
	uri, err := url.Parse(vaultURL)
	if err != nil {
		return nil, fmt.Errorf("parsing vault URL: %w", err)
	}

	esvHost := uri.Scheme + "://" + uri.Host
	err = c.runCommand(resp, esvHost, "describe-self", nil)
	if errors.Is(err, AuthError) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("describing self: %w", err)
	}
	return resp, nil
}

func (c *esvClient) SaveRefreshToken(ctx context.Context, refreshToken string) error {
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()
	c.refreshToken = refreshToken
	c.accessToken = ""

	err := ioutil.WriteFile(c.tokenPath, []byte(refreshToken), 0600)
	if err != nil {
		return fmt.Errorf("writing esv secret: %s", err)
	}
	return nil
}

func (c *esvClient) StartAuthFlow(ctx context.Context) (AuthResponse, error) {
	codeResponse, err := requestDeviceCode()
	if err != nil {
		return AuthResponse{}, fmt.Errorf("requesting device code: %w", err)
	}

	go func() {
		logger := logging.CurrentLogger(ctx)

		tokens, err := requestTokens(codeResponse.DeviceCode, codeResponse.Interval)
		if err != nil {
			logger.Infof("got error requesting tokens: %s", err)
			return
		}

		if err := c.SaveRefreshToken(ctx, tokens.RefreshToken); err != nil {
			logger.Infof("got error saving token: %s", err)
			return
		}
	}()

	return AuthResponse{
		AuthURL:  codeResponse.VerificationURIComplete,
		UserCode: codeResponse.UserCode,
	}, nil
}

func (c *esvClient) ensureAccessToken(host string) error {
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()

	// If we already have a valid access token, don't fetch a new one.
	if c.accessTokenExpiration.After(time.Now()) {
		return nil
	}

	if c.tokenPath == "" {
		return fmt.Errorf("token file not set")
	}
	if c.refreshToken == "" {
		tokenBytes, err := ioutil.ReadFile(c.tokenPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: token file does not exist", AuthError)
			}
			return fmt.Errorf("reading token file: %w", err)
		}
		c.refreshToken = strings.TrimSpace(string(tokenBytes))
	}

	result, err := getNewAccessToken(host, c.refreshToken)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	c.accessToken = result.AccessToken
	c.accessTokenExpiration = result.Expiry
	return nil
}

func (c *esvClient) runCommand(output interface{}, host, commandName string, body interface{}) error {
	marshalledBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshalling command body: %w", err)
	}

	if err := c.ensureAccessToken(host); err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	req, err := http.NewRequest("POST", host+"/api/_exo/"+commandName, bytes.NewBuffer(marshalledBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("making token request: %w", err)
	}

	if resp.StatusCode == 401 {
		return fmt.Errorf("running command %q: %w", commandName, AuthError)
	}

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	result, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading command result: %w", err)
	}

	if err := json.Unmarshal(result, output); err != nil {
		return fmt.Errorf("unmarshalling command result: %w", err)
	}

	return nil
}

func (c *esvClient) GetWorkspaceSecrets(vaultURL string) (map[string]string, error) {
	type describeVaultResp struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Secrets     []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Value       string `json:"value"`
		} `json:"secrets"`
	}

	organizationID, vaultID, err := getIDsFromURL(vaultURL)
	if err != nil {
		return nil, fmt.Errorf("could not find IDs: %w", err)
	}

	uri, err := url.Parse(vaultURL)
	if err != nil {
		return nil, fmt.Errorf("parsing secrets URL: %w", err)
	}
	host := url.URL{Scheme: uri.Scheme, Host: uri.Host}

	resp := describeVaultResp{}
	err = c.runCommand(&resp, host.String(), "describe-project", map[string]string{
		"organizationId": organizationID,
		"vaultId":        vaultID,
	})
	if err != nil {
		return nil, fmt.Errorf("running describe-project command: %w", err)
	}
	secrets := map[string]string{}
	for _, secret := range resp.Secrets {
		secrets[secret.DisplayName] = secret.Value
	}
	return secrets, nil
}

func getIDsFromURL(vaultURL string) (organizationID, vaultID string, err error) {
	parsedUrl, err := url.Parse(vaultURL)
	if err != nil {
		return "", "", fmt.Errorf("parsing vault URL: %w", err)
	}

	parts := strings.Split(parsedUrl.Path, "/")
	for i, part := range parts {
		if part == "organizations" {
			if i+1 < len(parts) {
				organizationID = parts[i+1]
			}
		}
		if part == "vaults" {
			if i+1 < len(parts) {
				vaultID = parts[i+1]
			}
		}
		if organizationID != "" && vaultID != "" {
			return
		}
	}
	err = fmt.Errorf("could not find IDs in URL: %q", vaultURL)
	return
}
