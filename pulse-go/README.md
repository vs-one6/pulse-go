# Pulse-Go 🚀

A lightweight container runtime written in Go that implements core containerization concepts using Linux namespaces, cgroups, and OCI image specifications.

## Overview

Pulse-Go is an educational container runtime that demonstrates how modern container technologies work under the hood. It provides Docker-like functionality for pulling images, running containers with isolation, and managing container networking.

## Features

- 🐳 **OCI Image Support**: Pull and run standard Docker/OCI images from registries
- 🔒 **Process Isolation**: Uses Linux namespaces (PID, Mount, UTS, IPC, Network, User)
- 🌐 **Container Networking**: Virtual ethernet pairs with bridge networking and NAT
- 📦 **Image Management**: Pull, list, and remove container images
- 🛠️ **Interactive Mode**: Run containers with TTY support
- 🔧 **Daemon Mode**: Background daemon for managing containers
- 🌍 **DNS Resolution**: Automatic DNS configuration for containers

## Architecture

Pulse-Go consists of two main components:

1. **`pulse`** - CLI client for interacting with containers
2. **`pulsed`** - Background daemon that manages container lifecycle

## Installation

### Prerequisites

- Go 1.23.3 or later
- Linux operating system (uses Linux-specific syscalls)
- Root privileges for networking features

### Build from Source

```bash
# Clone the repository
git clone https://github.com/vishnucs/pulse-go
cd pulse-go

# Build the binaries
go build -o bin/pulse ./cmd/pulse
go build -o bin/pulsed ./cmd/pulsed

# Optional: Install to system path
sudo cp bin/pulse /usr/local/bin/
sudo cp bin/pulsed /usr/local/bin/
```

## Usage

### Basic Commands

#### Pull an Image

```bash
pulse pull alpine
pulse pull ubuntu:22.04
```

#### List Images

```bash
pulse images
```

#### Run a Container

```bash
# Run a simple command
pulse run alpine echo "Hello from container"

# Run interactive shell (requires sudo for networking)
sudo pulse run -i -n alpine

# Run with custom command
pulse run alpine ls -la /

# Run with environment variables
pulse run -e FOO=bar -e BAZ=qux alpine env
```

#### Remove an Image

```bash
pulse remove alpine
```

### Advanced Usage

#### Interactive Mode with Networking

```bash
# Start container with network access and interactive shell
sudo pulse run -i -n ubuntu:22.04

# Inside the container, you can:
# - Install packages: apt update && apt install curl
# - Access the internet: curl https://google.com
# - Use DNS resolution
```

#### Running the Daemon

```bash
# Start the daemon in the background
pulsed &

# The daemon listens on /tmp/pulse.sock
# You can now use pulse commands without -i flag
```

## Core Concepts Learned

### 1. **Linux Namespaces**

Namespaces provide process isolation by creating separate instances of global system resources:

- **PID Namespace** (`CLONE_NEWPID`): Isolates process IDs, making the container process PID 1
- **Mount Namespace** (`CLONE_NEWNS`): Provides isolated filesystem view with separate mount points
- **UTS Namespace** (`CLONE_NEWUTS`): Isolates hostname and domain name
- **IPC Namespace** (`CLONE_NEWIPC`): Isolates inter-process communication resources
- **Network Namespace** (`CLONE_NEWNET`): Provides isolated network stack (interfaces, routing tables, firewall rules)
- **User Namespace** (`CLONE_NEWUSER`): Maps UIDs/GIDs for unprivileged containers

**Implementation**: See [`internals/container.go:239-252`](file:///home/vishnucs/pulse-go/internals/container.go#L239-L252)

### 2. **Chroot and Filesystem Isolation**

The `chroot` syscall changes the root directory for a process, creating filesystem isolation:

```go
syscall.Chroot(rootfs)  // Change root to container filesystem
os.Chdir("/")           // Change to new root
```

**Key Mounts**:
- `/proc` - Process information filesystem
- `/sys` - System device information
- `/tmp` - Temporary filesystem (tmpfs)
- `/dev` - Device files (bind-mounted from host)

**Implementation**: See [`internals/container.go:533-595`](file:///home/vishnucs/pulse-go/internals/container.go#L533-L595)

### 3. **Container Networking**

Pulse-Go implements container networking using:

#### Bridge Network
- Creates a virtual bridge (`pulse0`) with subnet `172.18.0.0/24`
- Bridge acts as a virtual switch connecting containers

#### Virtual Ethernet Pairs (veth)
- Creates paired virtual network interfaces
- One end attached to host bridge, other moved to container namespace
- Each container gets unique IP address (172.18.0.2-254)

#### NAT and IP Forwarding
- Uses iptables MASQUERADE for outbound traffic
- Enables IP forwarding in kernel
- Allows containers to access external networks

**Implementation**: See [`internals/container.go:18-183`](file:///home/vishnucs/pulse-go/internals/container.go#L18-L183)

### 4. **OCI Image Specification**

Pulse-Go works with OCI (Open Container Initiative) images:

#### Image Structure
```
image-oci/
├── index.json          # Points to manifest
├── blobs/
│   └── sha256/
│       ├── <manifest>  # Image configuration
│       └── <layers>    # Compressed filesystem layers
```

#### Image Pulling
- Uses `github.com/containers/image/v5` library
- Downloads from Docker registries (docker.io, etc.)
- Stores images in `~/.pulse/images/`

**Implementation**: See [`internals/pullImage.go`](file:///home/vishnucs/pulse-go/internals/pullImage.go)

#### Layer Extraction
- Reads OCI manifest to find filesystem layers
- Extracts tar.gz layers sequentially
- Overlays layers to create final rootfs

**Implementation**: See [`internals/extract.go`](file:///home/vishnucs/pulse-go/internals/extract.go)

### 5. **Process Execution and Isolation**

#### Fork-Exec Pattern
Pulse-Go uses a two-stage process:

1. **Parent Process**: Creates namespaces and sets up environment
2. **Child Process**: Executes inside isolated namespaces

```go
cmd := exec.Command("/proc/self/exe", append([]string{"child"}, command...)...)
cmd.SysProcAttr = &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | ...
}
```

**Implementation**: See [`internals/container.go:192-299`](file:///home/vishnucs/pulse-go/internals/container.go#L192-L299)

### 6. **DNS Resolution**

Containers need DNS to resolve domain names:

- Copies host's `/etc/resolv.conf` to container
- Filters out localhost addresses (systemd-resolved stub)
- Falls back to public DNS (8.8.8.8, 8.8.4.4) if needed

**Implementation**: See [`internals/container.go:427-486`](file:///home/vishnucs/pulse-go/internals/container.go#L427-L486)

### 7. **User Namespace Mapping**

For unprivileged containers, user namespaces map UIDs/GIDs:

```go
UidMappings: []syscall.SysProcIDMap{
    {ContainerID: 0, HostID: hostUID, Size: 65536},
}
```

- Container root (UID 0) maps to unprivileged host user
- When running with sudo, user namespace is skipped for real root privileges

**Implementation**: See [`internals/container.go:254-272`](file:///home/vishnucs/pulse-go/internals/container.go#L254-L272)

### 8. **Permission Management**

Handles complex permission scenarios:

- **Running with sudo**: Ensures rootfs is accessible and owned correctly
- **Path traversability**: Makes parent directories readable/executable
- **Ownership fixing**: Restores original user ownership after container exits

**Implementation**: See [`internals/container.go:301-366`](file:///home/vishnucs/pulse-go/internals/container.go#L301-L366)

## Project Structure

```
pulse-go/
├── cmd/
│   ├── pulse/          # CLI client
│   │   ├── main.go
│   │   ├── run.go      # Container run command
│   │   ├── pull.go     # Image pull command
│   │   ├── images.go   # List images command
│   │   └── remove.go   # Remove image command
│   └── pulsed/         # Daemon
│       └── main.go
├── internals/
│   ├── container.go    # Core container runtime logic
│   ├── pullImage.go    # OCI image pulling
│   ├── extract.go      # Image extraction
│   ├── extractTar.go   # Tar layer extraction
│   └── RemoveImage.go  # Image removal
├── go.mod
└── README.md
```

## Technical Details

### System Calls Used

- `clone()` - Create child process with namespaces
- `chroot()` - Change root directory
- `mount()` - Mount filesystems
- `sethostname()` - Set container hostname
- `exec()` - Execute container process

### Dependencies

- **github.com/containers/image/v5**: OCI image handling
- **github.com/spf13/cobra**: CLI framework

### Storage Locations

- **Images**: `~/.pulse/images/`
- **Daemon Socket**: `/tmp/pulse.sock`

## Limitations

- Linux-only (uses Linux-specific syscalls)
- No cgroup resource limits (CPU, memory)
- No overlay filesystem (uses direct extraction)
- Basic networking (no custom networks or port mapping)
- No container persistence or state management
- No volume mounting support

## Security Considerations

⚠️ **This is an educational project** and should not be used in production:

- No security auditing
- Simplified permission model
- No AppArmor/SELinux integration
- No seccomp filtering
- Root privileges required for networking


## Contributing

This is an educational project. Feel free to:
- Report issues
- Suggest improvements
- Submit pull requests
- Use it for learning



