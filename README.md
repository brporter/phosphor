```
 ██████╗ ██╗  ██╗ ██████╗ ███████╗██████╗ ██╗  ██╗ ██████╗ ██████╗
 ██╔══██╗██║  ██║██╔═══██╗██╔════╝██╔══██╗██║  ██║██╔═══██╗██╔══██╗
 ██████╔╝███████║██║   ██║███████╗██████╔╝███████║██║   ██║██████╔╝
 ██╔═══╝ ██╔══██║██║   ██║╚════██║██╔═══╝ ██╔══██║██║   ██║██╔══██╗
 ██║     ██║  ██║╚██████╔╝███████║██║     ██║  ██║╚██████╔╝██║  ██║
 ╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝
```
What if all of your machines were accessible wherever you went, without exposing SSH ports to the internet? What if machines in your homelab, behind dynamic IP's, could be accessed from your phone, tablet, or laptop, wherever you were on earth? Securely? Without complicated VPN setups?

This is why I build Phosphor.

Phosphor is a web-based terminal aggregator that allows you to securely connect to machines irrespective of where they are.

Phosphor inverts the traditional terminal access model. Instead of SSH'ing into a machine, your machines connect to a Phosphor hub. You can access this hub using federated credentials from Google, Microsoft or Apple, where you can then access and interact with terminal sessions on your machines.

Access to your machines is secured using federated credentials. The Phosphor hub itself stores no credentials; instead, it relies on well-known identity providers (e.g. Google, Microsoft and Apple).

Perfect for developers, system administrators, and enthusiasts who need terminal access to multiple machines without needing to expose those machines to the internet. Access your homelab or cloud resources from anywhere, without configuring VPN or exposing SSH ports to the open web!

Phosphor is open source, and completely self-hostable. By default, phosphor connects to the hub at phosphor.betaporter.dev, but you can easily set up your own hub if you prefer.

# Getting Started
To install Phosphor, download the latest release from the [releases page](https://github.com/brporter/phosphor/releases) that's appropriate for your operating system.

If running phosphor in [command mode](#command-mode), you must first login to the phosphor hub using the `phosphor login` command. This will open a browser window where you can authenticate with your identity provider. Device-code authentication is also support for Microsoft and Google authentication.

Once you've authenticated, your token is cached locally and used by the `phosphor` command to authenticate with the phosphor hub.

If running phosphor in [daemon mode](#daemon-mode), you must first login to the phosphor hub by opening a browser and navigating to [https://phosphor.betaporter.dev](https://phosphor.betaporter.dev). Once signed in, choose `settings` from the upper right hand corner and create an API key. API keys are not stored centrally; make a note of the generate API key, as it will not be retrievable or viewable again.

Install the phosphor daemon on your machine by running `phosphor daemon install`. Configure the daemon to use your API key by running `phosphor daemon set-key <API KEY>`.

The phosphor daemon registers itself as a systemd service on Linux, a launchd service on macOS, and a Windows service on Windows. Start the phosphor daemond using the usual commands on the platform of your choice.

* On Linux, `sudo systemctl start phosphor-daemon`
* On macOS, `sudo launchctl start com.betaporter.phosphor`
* On Windows, `Start-Service phosphor-daemon`

Map users to the phosphor daemon using the `phosphor daemon map` command. This will allow the users you specify to connect to your machine using the federated credentials specified and the identity mappings you specify.

Daemon mode is ideal for machines that need to be accessed by multiple users, and when you want to always have access to the machine remotely. Use daemon mode to make a machine in your home lab accessible remotely; or a cloud-hosted VM accessible without exposing SSH ports on the internet! 

Read on for more information about the various modes.

# Modes
Phosphor supports two modes of operations

## Command Mode
In this mode, you can either pipe the output of a process to Phosphor, in which case the session is read-only, or you can wrap the process in Phosphor for interactivity.

For example:

```phosphor -- vim foobar.txt```

Will wrap the vim process in Phosphor, associating the session with the logged-in users credentials, and allowing you to interact with vim both locally or through the Web-based terminal at phosphor.betaporter.dev. When you log in to phosphor.betaporter.dev with the same credentials you used to start the phosphor session, you'll see a listing of your sessions and can connect to them.

Alternatively, if you'd just like the output of a process sent to phosphor, you can pipe a commands output to phosphor. For example:

```tail -f /var/log/syslog | phosphor```

If you log-in to phosphor.betaporter.dev, you'll see a session that will allow you to view the output of the process - in this case `tail`. The process cannot be interacted with in this mode.

## Daemon Mode
In this mode, you can start a phosphor daemon on a machine and it will automatically create and manage sessions on demand for mapped users.

For example, if you have a Raspberry Pi with a local user named 'brporter', you can start the phosphor daemon on that machine and map your federated identity credential to that local user.

```phosphor daemon map --user brporter --identity bryan@bryanporter.com```

This associates the local user 'brporter' with the federated identity 'bryan@bryanporter.com'. When you log in to phosphor.betaporter.dev with the federated identity 'bryan@bryanporter.com', you'll see a listing of the sessions associated with that credential, can select one and join that session.

# Security
Phosphor connects to the Phosphor hub using a secure WebSocket connection. The hub itself does not store any credentials. Phosphor instances running in daemon mode must leverage an API key to connect to a Phosphor hub. These API keys are JWT tokens issued by the Phosphor hub, and can be revoked by an administrator.

By default, the Phosphor client connects to phosphor.betaporter.dev, but you can also self-host your own Phosphor hub if you prefer. Pass the `--relay` argument to Phosphor to connect a different hub.

