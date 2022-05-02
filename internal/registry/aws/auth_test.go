package aws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/google/go-containerregistry/pkg/authn"
	. "github.com/onsi/gomega"
)

const (
	testValidECRImage = "012345678901.dkr.ecr.us-east-1.amazonaws.com/foo:v1"
)

func TestParseImage(t *testing.T) {
	tests := []struct {
		image         string
		wantAccountID string
		wantRegion    string
		wantOK        bool
	}{
		{
			image:         "012345678901.dkr.ecr.us-east-1.amazonaws.com/foo:v1",
			wantAccountID: "012345678901",
			wantRegion:    "us-east-1",
			wantOK:        true,
		},
		{
			image:         "012345678901.dkr.ecr.us-east-1.amazonaws.com/foo",
			wantAccountID: "012345678901",
			wantRegion:    "us-east-1",
			wantOK:        true,
		},
		{
			image:  "012345678901.dkr.ecr.us-east-1.amazonaws.com",
			wantOK: false,
		},
		{
			image:  "gcr.io/foo/bar:baz",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			g := NewWithT(t)

			accId, region, ok := ParseImage(tt.image)
			g.Expect(ok).To(Equal(tt.wantOK), "unexpected OK")
			g.Expect(accId).To(Equal(tt.wantAccountID), "unexpected account IDs")
			g.Expect(region).To(Equal(tt.wantRegion), "unexpected regions")
		})
	}
}

func TestGetLoginAuth(t *testing.T) {
	tests := []struct {
		name           string
		responseBody   []byte
		statusCode     int
		wantErr        bool
		wantAuthConfig authn.AuthConfig
	}{
		{
			// NOTE: The authorizationToken is base64 encoded.
			name: "success",
			responseBody: []byte(`{
	"authorizationData": [
		{
			"authorizationToken": "c29tZS1rZXk6c29tZS1zZWNyZXQ="
		}
	]
}`),
			statusCode: http.StatusOK,
			wantAuthConfig: authn.AuthConfig{
				Username: "some-key",
				Password: "some-secret",
			},
		},
		{
			name:       "fail",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name: "invalid token",
			responseBody: []byte(`{
	"authorizationData": [
		{
			"authorizationToken": "c29tZS10b2tlbg=="
		}
	]
}`),
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name: "invalid data",
			responseBody: []byte(`{
	"authorizationData": [
		{
			"foo": "bar"
		}
	]
}`),
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:         "invalid response",
			responseBody: []byte(`{}`),
			statusCode:   http.StatusOK,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			handler := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}
			srv := httptest.NewServer(http.HandlerFunc(handler))
			t.Cleanup(func() {
				srv.Close()
			})

			// Configure the client.
			ec := NewClient()
			ec.Config = ec.WithEndpoint(srv.URL).
				WithCredentials(credentials.NewStaticCredentials("x", "y", "z"))

			a, err := ec.getLoginAuth("some-account-id", "us-east-1")
			g.Expect(err != nil).To(Equal(tt.wantErr))
			if tt.statusCode == http.StatusOK {
				g.Expect(a).To(Equal(tt.wantAuthConfig))
			}
		})
	}
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name       string
		autoLogin  bool
		image      string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "no auto login",
			autoLogin:  false,
			image:      testValidECRImage,
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "with auto login",
			autoLogin:  true,
			image:      testValidECRImage,
			statusCode: http.StatusOK,
		},
		{
			name:       "login failure",
			autoLogin:  true,
			image:      testValidECRImage,
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name:       "non ECR image",
			autoLogin:  true,
			image:      "gcr.io/foo/bar:v1",
			statusCode: http.StatusOK,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			handler := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(`{"authorizationData": [{"authorizationToken": "c29tZS1rZXk6c29tZS1zZWNyZXQ="}]}`))
			}
			srv := httptest.NewServer(http.HandlerFunc(handler))
			t.Cleanup(func() {
				srv.Close()
			})

			ecrClient := NewClient()
			ecrClient.Config = ecrClient.WithEndpoint(srv.URL).
				WithCredentials(credentials.NewStaticCredentials("x", "y", "z"))

			_, err := ecrClient.Login(context.TODO(), tt.autoLogin, tt.image)
			g.Expect(err != nil).To(Equal(tt.wantErr))
		})
	}
}
