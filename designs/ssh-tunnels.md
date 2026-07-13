# Phosphor and SSH Tunnels
The existing design of Phosphor has several limitations:

1. Session management doesn't work well - the phosphor client creates a session that a user connects to.
2. User mapping is manual, and must be done ahead of time.
3. End-to-end encryption requires a pre-shared secret that's one-off and special

This design document describes a fundamental design change that solves these problems.

# SSH Tunnels To The Rescue!
Phosphor v.Next will be built on top of ssh tunnels exclusively. This will consist of several components:

1. A `phosphor` command line application that can run interactively, or as a daemon.
2. A backend server that accepts SSH connections.
3. A frontend that authenticates users, presents them lists of machines associated with their account, and then allows them to connect to those machines.
4. Connecting to a machine will trigger an SSH session from the users browser, to the backend hosted at Phosphor, which will then use the tunnel created by the phosphor command line application to connect to the remote SSH daemon.
5. At no point will Phosphor have access to the session contents.

# phosphor CLI
The Phosphor CLI will use the x/crypto/ssh package to establish a reverse tunnel to the Phosphor backend. The CLI will expose the local SSH daemon on the host running the phosphor CLI over that tunnel to the backend.

# phosphor Backend / Frontend
The backend will accept SSH connections from the phosphor CLI. It will use a custom PAM module to take connecting clients through an authentication flow that associates the incoming connection to a Phosphor tenant. Each tenant can have multiple users; users are federated identity only (e.g. Microsoft.com auth, Google auth, Apple auth) any user that connnects to the Phosphor frontend will see a list of machines who have established tunnels for that tenant.

Selecting a machine will launch a WASM compiled SSH client, which will connect from the users computer to the Phosphor backend. This client will send the users JWT to the Phosphor machine along with a host selector, identifying which host to connect to. Phosphor's backend will validate that the host selector (which identifies which machine) is associated with the user account; if so, it'll allow the connection. If not, it'll block it. This will require users to use the web-based tool for connecting to remote machines; connecting with normal SSH clients won't work.

