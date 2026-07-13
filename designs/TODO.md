# TODO

## macOS Setup
On macOS, sshd is not started by default. Running phosphor will not work unless sshd is configured.

Add a check during execution of `phosphor tunnel` that inspects the macOS configuration, and if it cannot work, report
this to the user along with instructions on how to correct this configuration.

## Setup Walkthrough
Configuring a client is too cumbersome; build a mechanism to walk users through the configuration of the phosphor
client, from establishing a tenant, fetching an API key, etc. Maybe use a QR code at the terminal to kickstart the
process and establish machine / tenant relationship.
