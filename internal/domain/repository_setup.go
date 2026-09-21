package domain

type RepositorySetupState string

const (
	RepositorySetupUnregistered RepositorySetupState = "unregistered"
	RepositorySetupRegistered   RepositorySetupState = "registered"
	RepositorySetupReady        RepositorySetupState = "ready"
)

type RepositorySetupMode string

const (
	RepositorySetupInit   RepositorySetupMode = "init"
	RepositorySetupRemote RepositorySetupMode = "remote"
	RepositorySetupAdopt  RepositorySetupMode = "adopt"
)

type RepositoryAuthType string

const (
	RepositoryAuthNone  RepositoryAuthType = "none"
	RepositoryAuthHTTPS RepositoryAuthType = "https"
	RepositoryAuthSSH   RepositoryAuthType = "ssh"
)

type RepositoryPathState string

const (
	RepositoryPathEmpty    RepositoryPathState = "empty"
	RepositoryPathWorktree RepositoryPathState = "worktree"
	RepositoryPathOccupied RepositoryPathState = "occupied"
	RepositoryPathInvalid  RepositoryPathState = "invalid"
)

type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type RemoteAuthenticationInput struct {
	Type     RepositoryAuthType `json:"type"`
	Username string             `json:"username,omitempty"`
	Secret   string             `json:"secret,omitempty"`
}

type RepositoryRemoteInput struct {
	URL            string                    `json:"url"`
	Authentication RemoteAuthenticationInput `json:"authentication"`
}

type RepositorySetupRequest struct {
	Mode                 RepositorySetupMode    `json:"mode"`
	Branch               string                 `json:"branch"`
	Author               GitIdentity            `json:"author"`
	Remote               *RepositoryRemoteInput `json:"remote,omitempty"`
	ManageExistingRemote bool                   `json:"manageExistingRemote,omitempty"`
}

type RemoteInspectionRequest struct {
	Remote RepositoryRemoteInput `json:"remote"`
}

type RepositoryRemoteRequest struct {
	Remote          RepositoryRemoteInput `json:"remote"`
	Branch          string                `json:"branch"`
	ReplaceExisting bool                  `json:"replaceExisting"`
}

type RepositoryAuthentication struct {
	Type           RepositoryAuthType `json:"-"`
	Username       string             `json:"-"`
	Secret         string             `json:"-"`
	SSHKeyPath     string             `json:"-"`
	KnownHostsPath string             `json:"-"`
}

type RepositoryRemoteSummary struct {
	Name     string             `json:"name"`
	URL      string             `json:"url"`
	AuthType RepositoryAuthType `json:"authType"`
	Managed  bool               `json:"managed"`
}

type RepositoryConfiguration struct {
	State  RepositorySetupState
	Root   string
	Branch string
	Author GitIdentity
	Remote *RepositoryRemoteSummary
}

type RepositoryModeAvailability struct {
	Mode      RepositorySetupMode `json:"mode"`
	Available bool                `json:"available"`
	Reason    string              `json:"reason,omitempty"`
}

type SSHMaterialStatus struct {
	IdentityAvailable   bool `json:"identityAvailable"`
	KnownHostsAvailable bool `json:"knownHostsAvailable"`
	Usable              bool `json:"usable"`
}

type RepositorySetupStatus struct {
	State          RepositorySetupState         `json:"state"`
	Required       bool                         `json:"required"`
	PathState      RepositoryPathState          `json:"pathState"`
	Modes          []RepositoryModeAvailability `json:"modes"`
	Branch         string                       `json:"branch,omitempty"`
	Author         GitIdentity                  `json:"author"`
	DefaultAuthor  GitIdentity                  `json:"defaultAuthor"`
	ExistingRemote *RepositoryRemoteSummary     `json:"existingRemote,omitempty"`
	ManagedRemote  *RepositoryRemoteSummary     `json:"managedRemote,omitempty"`
	SSH            SSHMaterialStatus            `json:"ssh"`
}

type RemoteInspection struct {
	RemoteURL     string   `json:"remoteUrl"`
	DefaultBranch string   `json:"defaultBranch,omitempty"`
	Branches      []string `json:"branches"`
	Empty         bool     `json:"empty"`
	Suggested     string   `json:"suggestedBranch"`
}

type RepositoryPathInspection struct {
	State          RepositoryPathState
	Branch         string
	Author         GitIdentity
	ExistingRemote *RepositoryRemoteSummary
	Detached       bool
	Reason         string
}
