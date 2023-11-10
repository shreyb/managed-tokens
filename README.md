[![Go Report Card](https://goreportcard.com/badge/github.com/shreyb/managed-tokens)](https://goreportcard.com/report/github.com/shreyb/managed-tokens)
![Go build and test](https://github.com/shreyb/managed-tokens/actions/workflows/go.yml/badge.svg)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/shreyb/managed-tokens)](https://pkg.go.dev/github.com/shreyb/managed-tokens)


# managed-tokens
Managed Tokens service for FIFE Experiments at Fermilab

The Managed Tokens Service stores and distributes HashiCorp Vault tokens for the FIFE experiments to use in their production activities. Specifically, the Managed Tokens service enables experiments to automate grid activities such as job submission and file transfers by ensuring that the valid credentials (Vault Tokens) always exist on experiment interactive nodes, ready to be used.

# Executables
The Managed Tokens Service consists of three executables:

* `token-push`: Executable that uses the service keytabs to generate vault tokens, store them on [HTCondor](https://htcondor.org/) credd machines, and push the vault tokens to the experiment interactive nodes. By default, this runs every hour.
* `refresh-uids-from-ferry`: Executable that queries FERRY (the credentials and grid mapping registry service at Fermilab) to pull down the applicable UIDs for the configured UNIX accounts. By default, this runs daily each morning.
* `run-onboarding-managed-tokens`: A lightweight wrapper around condor_vault_storer that must be run when onboarding a new experiment or experiment account to the Managed Tokens Service. In lieu of this, the operator may run condor_vault_storer [experiment]_[role].

The `token-push` executable will copy the vault token to the destination nodes at two locations:

* `/tmp/vt_u<UID>`
* `/tmp/vt_u<UID>-<service>`

# Notifications and Experiment-Specific Emails

The Managed Tokens Service, under the default mode, will send errors and pertinent warnings to three places:

* Experiment-specific alerts will go to the recipients specified in the emails entry for an experiment in configuration file.
* All alerts will get sent to configured `admin_email` (logging level ERROR or above).
* All alerts will get sent to the configured slack channel

# Logs

The logfiles for the Managed Tokens service are, by default, located in the /var/log/managed-tokens directory (configurable). Each executable has its own log and debug log, and these are rotated periodically by default if installed via RPM.

# Metrics

These are the current prometheus metrics that can be pushed from the Managed Tokens executables to a [prometheus pushgateway](https://prometheus.io/docs/practices/pushing/) configured at the `prometheus.host` entry in the configuration file. These are:

## General executable-level metrics
* `managed_tokens_stage_duration_seconds`:  Per executable, per stage (setup, processing, cleanup).  How long each stage took to run.

## refresh-uids-from-ferry-specific metrics

* `managed_tokens_last_ferry_refresh`: Timestamp of when refresh-uids-from-ferry executable last got information from FERRY.
* `managed_tokens_ferry_request_duration_seconds`: The amount of time it took in seconds to make a request to FERRY and receive the response
* `managed_tokens_ferry_request_error_count`: The number of requests to FERRY that failed

## token-push-specific metrics

* `managed_tokens_failed_services_push_count`:  Count of how many services registered a failure to push a vault token to a node in the current run of token-push.  Basically, a failure count.

## Internal library metrics

### Kerberos metrics
* `managed_tokens_kinit_duration_seconds`: Duration (in seconds) for a kerberos ticket to be created from the service principal
* `managed_tokens_failed_kinit_count`: The number of times the Managed Tokens Service failed to create a kerberos ticket from the service principal

### Vault Token Store metrics
* `managed_tokens_last_token_store_timestamp`: Timestamp of the last successful store of a service vault token in a condor credd by the Managed Tokens Service
* `managed_tokens_token_store_duration_seconds`: Duration (in seconds) for a vault token to get stored in a condor credd
* `managed_tokens_failed_vault_token_store_count`: The number of times the Managed Tokens Service failed to store a vault token in a condor credd

### Node-pinging metrics
* `managed_tokens_ping_duration_seconds`: Duration (in seconds) to ping a node
* `managed_tokens_failed_ping_count`: The number of times the Managed Tokens Service failed to ping a node

### Pushing tokens metrics
* `managed_tokens_last_token_push_timestamp`: Timestamp of when token-push last pushed a particular service vault token to a particular node.
* `managed_tokens_token_push_duration_seconds`: Duration (in seconds) for a vault token to get pushed to a node
* `managed_tokens_failed_token_push_count`: The number of times the Managed Tokens service failed to push a token to an interactive node


### Notification-sending metrics
* `managed_tokens_admin_error_email_last_sent_timestamp`:  The last time managed tokens service attempted to send an admin error notification
* `managed_tokens_admin_error_email_send_duration_seconds`: Time in seconds it took to successfully send an admin error email
* `managed_tokens_service_error_email_last_sent_timestamp`: Last time managed tokens service attempted to send an service error notification
* `managed_tokens_service_error_email_send_duration_seconds`: Time in seconds it took to successfully send a service error email


### Error-count metrics (mirrors internal database state)
* `managed_tokens_current_setup_error_count`: Count of how many consecutive setup errors there have been for a single service.  Will reset to 0 after an error notification is sent
* `managed_tokens_current_push_error_count`: Count of how many consecutive push errors there have been for a single service/node combination.  Will reset to 0 after an error notification is sent
