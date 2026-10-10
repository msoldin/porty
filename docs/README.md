# Porty documentation

Porty brings Docker Compose, Git-backed configuration, and host monitoring into one interface for a single administrator on a Linux server.

## Start here

1. [Explore the features and limits](features.md).
2. [Install Porty and deploy your first stack](getting-started.md).
3. [Learn the everyday workflows](user-guide.md).

## Daily use

| I want to…                                  | Read                                                                                                         |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Understand host usage and missing readings  | [Dashboard](user-guide.md#dashboard)                                                                         |
| Create, find, rename, or delete a stack     | [Stacks](user-guide.md#stacks)                                                                               |
| Edit files and set environment values       | [Compose and files](user-guide.md#compose-and-files), [Environment values](user-guide.md#environment-values) |
| Apply configuration and inspect containers  | [Deployments](user-guide.md#deployments), [Containers and logs](user-guide.md#containers-and-logs)           |
| Commit and synchronize configuration        | [Git workflow](user-guide.md#git-workflow)                                                                   |
| Investigate a failed action                 | [Operations, alerts, and audit](user-guide.md#operations-alerts-and-audit)                                   |
| Schedule image updates                      | [Automatic stack updates](operator-guide.md#automatic-stack-updates)                                         |
| Stop idle services and wake them on traffic | [On-demand container groups](on-demand.md)                                                                   |
| Change a password or appearance             | [Account and appearance](user-guide.md#account-and-appearance)                                               |

## Administration and recovery

- [Operator guide](operator-guide.md): host and container installation, repository authentication, backup, restore, upgrades, and monitoring access.
- [Configuration reference](configuration.md): configuration files, flags, environment variables, defaults, and persistence.
- [Troubleshooting](troubleshooting.md): checks and recovery for common problems.
- [Security model](security.md): privileged access, authentication, and trust boundaries.

## Contributing

See the [development guide](development.md) for builds and tests and [repository guidelines](../AGENTS.md) for contribution requirements. User documentation changes belong with the feature or behavior change they explain.

[Back to the project README](../README.md)
