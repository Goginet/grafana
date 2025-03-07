package social

import (
	"net/http"
	"strings"

	"context"

	"golang.org/x/oauth2"

	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/setting"
	"github.com/grafana/grafana/pkg/util"
)

type BasicUserInfo struct {
	Id      string
	Name    string
	Email   string
	Login   string
	Company string
	Role    string
	Groups  []string
}

type DashboardAction string

const (
	UpdateDashboard DashboardAction = "update"
	CreateDashboard DashboardAction = "create"
	DeleteDashboard DashboardAction = "delete"
)

type UpdateDashboardOptions struct {
	Action    DashboardAction
	Message   string
	Title     string
	Name      string
	Dashboard string
	Folder    string
	OrgId     int64
}

type SocialConnector interface {
	Type() int
	UserInfo(client *http.Client, token *oauth2.Token) (*BasicUserInfo, error)
	IsEmailAllowed(email string) bool
	IsSignupAllowed() bool
	UpdateDashboard(options *UpdateDashboardOptions, token string) error

	AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string
	Exchange(ctx context.Context, code string, authOptions ...oauth2.AuthCodeOption) (*oauth2.Token, error)
	Client(ctx context.Context, t *oauth2.Token) *http.Client
	TokenSource(ctx context.Context, t *oauth2.Token) oauth2.TokenSource
}

func (s SocialBase) UpdateDashboard(options *UpdateDashboardOptions, token string) error {
	return nil
}

type SocialBase struct {
	*oauth2.Config
	log log.Logger
}

type Error struct {
	s string
}

func (e *Error) Error() string {
	return e.s
}

const (
	grafanaCom = "grafana_com"
)

var (
	SocialBaseUrl = "/login/"
	SocialMap     = make(map[string]SocialConnector)
	allOauthes    = []string{"github", "gitlab", "google", "generic_oauth", "grafananet", grafanaCom}
)

func NewOAuthService() {
	setting.OAuthService = &setting.OAuther{}
	setting.OAuthService.OAuthInfos = make(map[string]*setting.OAuthInfo)

	for _, name := range allOauthes {
		sec := setting.Raw.Section("auth." + name)
		info := &setting.OAuthInfo{
			ClientId:                     sec.Key("client_id").String(),
			ClientSecret:                 sec.Key("client_secret").String(),
			Scopes:                       util.SplitString(sec.Key("scopes").String()),
			AuthUrl:                      sec.Key("auth_url").String(),
			TokenUrl:                     sec.Key("token_url").String(),
			ApiUrl:                       sec.Key("api_url").String(),
			Enabled:                      sec.Key("enabled").MustBool(),
			EmailAttributeName:           sec.Key("email_attribute_name").String(),
			EmailAttributePath:           sec.Key("email_attribute_path").String(),
			AllowedDomains:               util.SplitString(sec.Key("allowed_domains").String()),
			HostedDomain:                 sec.Key("hosted_domain").String(),
			AllowSignup:                  sec.Key("allow_sign_up").MustBool(),
			Name:                         sec.Key("name").MustString(name),
			TlsClientCert:                sec.Key("tls_client_cert").String(),
			TlsClientKey:                 sec.Key("tls_client_key").String(),
			TlsClientCa:                  sec.Key("tls_client_ca").String(),
			TlsSkipVerify:                sec.Key("tls_skip_verify_insecure").MustBool(),
			SendClientCredentialsViaPost: sec.Key("send_client_credentials_via_post").MustBool(),
		}

		if !info.Enabled {
			continue
		}

		// handle the clients that do not properly support Basic auth headers and require passing client_id/client_secret via POST payload
		if info.SendClientCredentialsViaPost {
			// TODO: Fix the staticcheck error
			oauth2.RegisterBrokenAuthHeaderProvider(info.TokenUrl) //nolint:staticcheck
		}

		if name == "grafananet" {
			name = grafanaCom
		}

		setting.OAuthService.OAuthInfos[name] = info

		config := oauth2.Config{
			ClientID:     info.ClientId,
			ClientSecret: info.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  info.AuthUrl,
				TokenURL: info.TokenUrl,
			},
			RedirectURL: strings.TrimSuffix(setting.AppUrl, "/") + SocialBaseUrl + name,
			Scopes:      info.Scopes,
		}

		logger := log.New("oauth." + name)

		// GitHub.
		if name == "github" {
			SocialMap["github"] = &SocialGithub{
				SocialBase: &SocialBase{
					Config: &config,
					log:    logger,
				},
				allowedDomains:       info.AllowedDomains,
				apiUrl:               info.ApiUrl,
				allowSignup:          info.AllowSignup,
				teamIds:              sec.Key("team_ids").Ints(","),
				allowedOrganizations: util.SplitString(sec.Key("allowed_organizations").String()),
			}
		}

		// GitLab.
		if name == "gitlab" {
			reposSettings := setting.Raw.ChildSections("auth." + name + ".repo")
			var repos []*GrafanaGitlabRepo

			for _, repoSetting := range reposSettings {
				org_id, _ := repoSetting.Key("org_id").Int64()
				repo_id, _ := repoSetting.Key("repo_id").Int()

				repo := &GrafanaGitlabRepo{
					Branch:         repoSetting.Key("branch").String(),
					OrgId:          org_id,
					RepoId:         repo_id,
					DashboardsPath: repoSetting.Key("dashboards_path").String(),
					Url:            repoSetting.Key("url").String(),
				}

				repos = append(repos, repo)
			}

			SocialMap["gitlab"] = &SocialGitlab{
				SocialBase: &SocialBase{
					Config: &config,
					log:    logger,
				},
				allowedDomains: info.AllowedDomains,
				apiUrl:         info.ApiUrl,
				allowSignup:    info.AllowSignup,
				allowedGroups:  util.SplitString(sec.Key("allowed_groups").String()),
				repos:          repos,
			}
		}

		// Google.
		if name == "google" {
			SocialMap["google"] = &SocialGoogle{
				SocialBase: &SocialBase{
					Config: &config,
					log:    logger,
				},
				allowedDomains: info.AllowedDomains,
				hostedDomain:   info.HostedDomain,
				apiUrl:         info.ApiUrl,
				allowSignup:    info.AllowSignup,
			}
		}

		// Generic - Uses the same scheme as Github.
		if name == "generic_oauth" {
			SocialMap["generic_oauth"] = &SocialGenericOAuth{
				SocialBase: &SocialBase{
					Config: &config,
					log:    logger,
				},
				allowedDomains:       info.AllowedDomains,
				apiUrl:               info.ApiUrl,
				allowSignup:          info.AllowSignup,
				emailAttributeName:   info.EmailAttributeName,
				emailAttributePath:   info.EmailAttributePath,
				teamIds:              sec.Key("team_ids").Ints(","),
				allowedOrganizations: util.SplitString(sec.Key("allowed_organizations").String()),
			}
		}

		if name == grafanaCom {
			config = oauth2.Config{
				ClientID:     info.ClientId,
				ClientSecret: info.ClientSecret,
				Endpoint: oauth2.Endpoint{
					AuthURL:  setting.GrafanaComUrl + "/oauth2/authorize",
					TokenURL: setting.GrafanaComUrl + "/api/oauth2/token",
				},
				RedirectURL: strings.TrimSuffix(setting.AppUrl, "/") + SocialBaseUrl + name,
				Scopes:      info.Scopes,
			}

			SocialMap[grafanaCom] = &SocialGrafanaCom{
				SocialBase: &SocialBase{
					Config: &config,
					log:    logger,
				},
				url:                  setting.GrafanaComUrl,
				allowSignup:          info.AllowSignup,
				allowedOrganizations: util.SplitString(sec.Key("allowed_organizations").String()),
			}
		}
	}
}

// GetOAuthProviders returns available oauth providers and if they're enabled or not
var GetOAuthProviders = func(cfg *setting.Cfg) map[string]bool {
	result := map[string]bool{}

	if cfg == nil || cfg.Raw == nil {
		return result
	}

	for _, name := range allOauthes {
		if name == "grafananet" {
			name = grafanaCom
		}

		sec := cfg.Raw.Section("auth." + name)
		if sec == nil {
			continue
		}
		result[name] = sec.Key("enabled").MustBool()
	}

	return result
}
