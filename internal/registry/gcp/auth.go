package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/fluxcd/image-reflector-controller/internal/registry"
)

type gceToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// GCP_TOKEN_URL is the default GCP metadata endpoint used for authentication.
const GCP_TOKEN_URL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"

// ValidHost returns if a given host is a valid GCR host.
func ValidHost(host string) bool {
	return host == "gcr.io" || strings.HasSuffix(host, ".gcr.io") || strings.HasSuffix(host, "-docker.pkg.dev")
}

// Client is a GCP GCR client which can log into the registry and return
// authorization information.
type Client struct {
	tokenURL string
}

// NewClient creates a new GCR client with default configurations.
func NewClient() *Client {
	return &Client{tokenURL: GCP_TOKEN_URL}
}

// WithTokenURL sets the token URL used by the GCR client.
func (c *Client) WithTokenURL(url string) *Client {
	c.tokenURL = url
	return c
}

// getLoginAuth obtains authentication by getting a token from the metadata API
// on GCP. This assumes that the pod has right to pull the image which would be
// the case if it is hosted on GCP. It works with both service account and
// workload identity enabled clusters.
func (c *Client) getLoginAuth(ctx context.Context) (authn.AuthConfig, error) {
	var authConfig authn.AuthConfig

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.tokenURL, nil)
	if err != nil {
		return authConfig, err
	}

	request.Header.Add("Metadata-Flavor", "Google")

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return authConfig, err
	}
	defer io.Copy(io.Discard, response.Body)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return authConfig, fmt.Errorf("unexpected status from metadata service: %s", response.Status)
	}

	var accessToken gceToken
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&accessToken); err != nil {
		return authConfig, err
	}

	authConfig = authn.AuthConfig{
		Username: "oauth2accesstoken",
		Password: accessToken.AccessToken,
	}
	return authConfig, nil
}

// Login attempts to get the authentication material for GCR. The caller can
// ensure that the passed image is a valid GCR image using ValidHost().
func (c *Client) Login(ctx context.Context, autoLogin bool, image string, ref name.Reference) (authn.Authenticator, error) {
	if autoLogin {
		ctrl.LoggerFrom(ctx).Info("logging in to GCP GCR for " + image)
		authConfig, err := c.getLoginAuth(ctx)
		if err != nil {
			ctrl.LoggerFrom(ctx).Info("error logging into GCP " + err.Error())
			return nil, err
		}

		auth := authn.FromConfig(authConfig)
		return auth, nil
	}
	ctrl.LoggerFrom(ctx).Info("GCR authentication is not enabled. To enable, set the controller flag --gcp-autologin-for-gcr")
	return nil, fmt.Errorf("GCR authentication failed: %w", registry.ErrUnconfiguredProvider)
}
