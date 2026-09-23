package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type RepositoryCredentials struct{ Username, Secret string }

type RepositoryStore struct{ db *sql.DB }

func NewRepositoryStore(db *sql.DB) *RepositoryStore { return &RepositoryStore{db: db} }

func (s *RepositoryStore) Load(ctx context.Context) (domain.RepositoryConfiguration, domain.RepositoryAuthentication, error) {
	var configuration domain.RepositoryConfiguration
	var setupState string
	var root, remoteName, remoteURL, branch, authorName, authorEmail sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT setup_state,repository_root,remote_name,remote_url_redacted,tracking_branch,git_author_name,git_author_email FROM app_state WHERE id=1`).
		Scan(&setupState, &root, &remoteName, &remoteURL, &branch, &authorName, &authorEmail); err != nil {
		return domain.RepositoryConfiguration{}, domain.RepositoryAuthentication{}, err
	}
	configuration.State = domain.RepositorySetupState(setupState)
	configuration.Root = root.String
	configuration.Branch = branch.String
	configuration.Author = domain.GitIdentity{Name: authorName.String, Email: authorEmail.String}
	if remoteName.Valid || remoteURL.Valid {
		configuration.Remote = &domain.RepositoryRemoteSummary{
			Name:    remoteName.String,
			URL:     remoteURL.String,
			Managed: true,
		}
	}

	var authentication domain.RepositoryAuthentication
	var authType string
	var username, sshKeyPath, knownHostsPath sql.NullString
	var secret []byte
	err := s.db.QueryRowContext(ctx, `SELECT auth_type,https_username,https_secret,ssh_key_path,known_hosts_path FROM repository_auth WHERE id=1`).
		Scan(&authType, &username, &secret, &sshKeyPath, &knownHostsPath)
	if err == sql.ErrNoRows {
		authentication.Type = domain.RepositoryAuthNone
	} else if err != nil {
		return domain.RepositoryConfiguration{}, domain.RepositoryAuthentication{}, err
	} else {
		authentication.Type = domain.RepositoryAuthType(authType)
		switch authentication.Type {
		case domain.RepositoryAuthNone:
		case domain.RepositoryAuthHTTPS:
			authentication.Username = username.String
			authentication.Secret = string(secret)
		case domain.RepositoryAuthSSH:
			authentication.SSHKeyPath = sshKeyPath.String
			authentication.KnownHostsPath = knownHostsPath.String
		default:
			return domain.RepositoryConfiguration{}, domain.RepositoryAuthentication{}, fmt.Errorf("unknown repository authentication type %q", authType)
		}
	}
	if configuration.Remote != nil {
		configuration.Remote.AuthType = authentication.Type
	}
	return configuration, authentication, nil
}

func (s *RepositoryStore) Save(ctx context.Context, configuration domain.RepositoryConfiguration, authentication domain.RepositoryAuthentication) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var remoteName, remoteURL any
	if configuration.Remote != nil {
		remoteName = nullableString(configuration.Remote.Name)
		remoteURL = nullableString(configuration.Remote.URL)
	}
	now := encodeTime(time.Now().UTC())
	if _, err := tx.ExecContext(ctx, `UPDATE app_state SET setup_state=?,repository_root=?,remote_name=?,remote_url_redacted=?,tracking_branch=?,git_author_name=?,git_author_email=?,updated_at=? WHERE id=1`,
		string(configuration.State), nullableString(configuration.Root), remoteName, remoteURL, nullableString(configuration.Branch), nullableString(configuration.Author.Name), nullableString(configuration.Author.Email), now); err != nil {
		return err
	}

	var username, secret, sshKeyPath, knownHostsPath any
	switch authentication.Type {
	case domain.RepositoryAuthNone:
		if _, err := tx.ExecContext(ctx, `DELETE FROM repository_auth WHERE id=1`); err != nil {
			return err
		}
	case domain.RepositoryAuthHTTPS:
		username = nullableString(authentication.Username)
		secret = []byte(authentication.Secret)
	case domain.RepositoryAuthSSH:
		sshKeyPath = nullableString(authentication.SSHKeyPath)
		knownHostsPath = nullableString(authentication.KnownHostsPath)
	default:
		return fmt.Errorf("unknown repository authentication type %q", authentication.Type)
	}
	if authentication.Type != domain.RepositoryAuthNone {
		if _, err := tx.ExecContext(ctx, `INSERT INTO repository_auth(id,auth_type,https_username,https_secret,ssh_key_path,known_hosts_path,updated_at) VALUES(1,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET auth_type=excluded.auth_type,https_username=excluded.https_username,https_secret=excluded.https_secret,ssh_key_path=excluded.ssh_key_path,known_hosts_path=excluded.known_hosts_path,updated_at=excluded.updated_at`,
			string(authentication.Type), username, secret, sshKeyPath, knownHostsPath, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *RepositoryStore) SaveConfiguration(ctx context.Context, remote, branch, username, secret string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE app_state SET remote_name=?,remote_url_redacted=?,tracking_branch=?,updated_at=? WHERE id=1`, nullableString("origin"), nullableString(remote), branch, encodeTime(time.Now().UTC())); err != nil {
		return err
	}
	if username == "" && secret == "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM repository_auth WHERE id=1`); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO repository_auth(id,auth_type,https_username,https_secret,updated_at) VALUES(1,'https',?,?,?) ON CONFLICT(id) DO UPDATE SET auth_type='https',https_username=excluded.https_username,https_secret=excluded.https_secret,updated_at=excluded.updated_at`, username, []byte(secret), encodeTime(time.Now().UTC())); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *RepositoryStore) Credentials(ctx context.Context) (RepositoryCredentials, error) {
	var result RepositoryCredentials
	var secret []byte
	err := s.db.QueryRowContext(ctx, `SELECT https_username,https_secret FROM repository_auth WHERE id=1 AND auth_type='https'`).Scan(&result.Username, &secret)
	result.Secret = string(secret)
	return result, err
}

func (s *RepositoryStore) Branch(ctx context.Context) (string, error) {
	var branch sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT tracking_branch FROM app_state WHERE id=1`).Scan(&branch)
	if err != nil {
		return "", err
	}
	if branch.String == "" {
		return "main", nil
	}
	return branch.String, nil
}
