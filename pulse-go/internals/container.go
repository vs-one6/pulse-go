package internals

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	bridgeName = "pulse0"
	bridgeIP   = "172.18.0.1/24"
)

// SetupNetworking configures the network bridge (run once at daemon start)
func SetupNetworking() error {
	// Check if bridge already exists
	if _, err := net.InterfaceByName(bridgeName); err == nil {
		return nil // Bridge already exists
	}

	// Create bridge
	if err := exec.Command("ip", "link", "add", bridgeName, "type", "bridge").Run(); err != nil {
		return fmt.Errorf("failed to create bridge: %v", err)
	}

	// Assign IP to bridge
	if err := exec.Command("ip", "addr", "add", bridgeIP, "dev", bridgeName).Run(); err != nil {
		return fmt.Errorf("failed to assign IP to bridge: %v", err)
	}

	// Bring bridge up
	if err := exec.Command("ip", "link", "set", bridgeName, "up").Run(); err != nil {
		return fmt.Errorf("failed to bring bridge up: %v", err)
	}

	// Enable IP forwarding
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644); err != nil {
		return fmt.Errorf("failed to enable IP forwarding: %v", err)
	}

	// Set up NAT with iptables
	if err := setupNAT(); err != nil {
		return fmt.Errorf("failed to setup NAT: %v", err)
	}

	return nil
}

func setupNAT() error {
	// Get the default network interface
	defaultIface, err := getDefaultInterface()
	if err != nil {
		return err
	}

	// Check if rule already exists
	checkCmd := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING",
		"-s", "172.18.0.0/24", "-o", defaultIface, "-j", "MASQUERADE")
	if checkCmd.Run() == nil {
		return nil // Rule already exists
	}

	// Add MASQUERADE rule for NAT
	cmd := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", "172.18.0.0/24", "-o", defaultIface, "-j", "MASQUERADE")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add iptables rule: %v", err)
	}

	// Allow forwarding from bridge
	cmd = exec.Command("iptables", "-A", "FORWARD", "-i", bridgeName, "-j", "ACCEPT")
	cmd.Run() // Ignore error if rule exists

	cmd = exec.Command("iptables", "-A", "FORWARD", "-o", bridgeName, "-j", "ACCEPT")
	cmd.Run() // Ignore error if rule exists

	return nil
}

func getDefaultInterface() (string, error) {
	// Get default route
	cmd := exec.Command("ip", "route", "show", "default")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Parse output to find interface
	parts := splitFields(string(output))
	for i, part := range parts {
		if part == "dev" && i+1 < len(parts) {
			return parts[i+1], nil
		}
	}

	return "eth0", nil // fallback
}

func splitFields(s string) []string {
	var fields []string
	var current string
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' {
			if current != "" {
				fields = append(fields, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		fields = append(fields, current)
	}
	return fields
}

// ConfigureContainerNetwork sets up networking for a specific container
func ConfigureContainerNetwork(containerPID int, containerID string) error {
	// Generate unique veth pair names (handle short IDs)
	hostSuffix := containerID
	if len(containerID) > 8 {
		hostSuffix = containerID[:8]
	}
	containerSuffix := containerID
	if len(containerID) > 7 {
		containerSuffix = containerID[:7]
	}

	vethHost := fmt.Sprintf("veth%s", hostSuffix)
	vethContainer := fmt.Sprintf("vethc%s", containerSuffix)

	// Create veth pair
	cmd := exec.Command("ip", "link", "add", vethHost, "type", "veth", "peer", "name", vethContainer)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create veth pair: %v", err)
	}

	// Attach host side to bridge
	if err := exec.Command("ip", "link", "set", vethHost, "master", bridgeName).Run(); err != nil {
		return fmt.Errorf("failed to attach veth to bridge: %v", err)
	}

	// Bring up host side
	if err := exec.Command("ip", "link", "set", vethHost, "up").Run(); err != nil {
		return fmt.Errorf("failed to bring up host veth: %v", err)
	}

	// Move container side to container network namespace
	if err := exec.Command("ip", "link", "set", vethContainer, "netns", fmt.Sprintf("%d", containerPID)).Run(); err != nil {
		return fmt.Errorf("failed to move veth to container: %v", err)
	}

	// Configure container side (inside the namespace)
	netnsPath := fmt.Sprintf("/proc/%d/ns/net", containerPID)

	// Assign IP to container interface
	containerIP := fmt.Sprintf("172.18.0.%d/24", 2+(containerPID%253))
	if err := execInNetNS(netnsPath, "ip", "addr", "add", containerIP, "dev", vethContainer); err != nil {
		return fmt.Errorf("failed to assign IP: %v", err)
	}

	// Bring up container interface
	if err := execInNetNS(netnsPath, "ip", "link", "set", vethContainer, "up"); err != nil {
		return fmt.Errorf("failed to bring up container veth: %v", err)
	}

	// Bring up loopback
	if err := execInNetNS(netnsPath, "ip", "link", "set", "lo", "up"); err != nil {
		return fmt.Errorf("failed to bring up loopback: %v", err)
	}

	// Set default route
	if err := execInNetNS(netnsPath, "ip", "route", "add", "default", "via", "172.18.0.1"); err != nil {
		return fmt.Errorf("failed to set default route: %v", err)
	}

	return nil
}

func execInNetNS(netnsPath string, args ...string) error {
	// Use nsenter to execute command in network namespace
	cmdArgs := append([]string{"--net=" + netnsPath}, args...)
	cmd := exec.Command("nsenter", cmdArgs...)
	return cmd.Run()
}

func RunContainer(rootfs string, command []string, envVars []string, network bool, interactive bool) error {
	// When running with sudo, ensure rootfs directories are accessible
	if os.Geteuid() == 0 && os.Getenv("SUDO_UID") != "" {
		// Make the path traversable for the container process
		makePathTraversable(rootfs)

		// Also ensure the rootfs itself is owned by root so apt/etc can work
		// This is necessary because the image was extracted as the user
		// but the container runs as real root (no user namespace)
		ensureRootOwnership(rootfs)
	}

	// Setup DNS before starting container (in parent process with proper permissions)
	if network {
		if err := setupDNS(rootfs); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to setup DNS: %v\n", err)
		}
	}

	if len(command) == 0 {
		shells := []string{
			"/usr/bin/bash",
			"/bin/bash",
			"/usr/bin/sh",
			"/bin/sh",
			"/bin/ash",
			"/bin/dash",
		}
		command = []string{"/bin/sh"}

		for _, shell := range shells {
			shellPath := filepath.Join(rootfs, shell[1:])
			if info, err := os.Stat(shellPath); err == nil && !info.IsDir() {
				command = []string{shell}
				break
			}
		}
	}

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, command...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Env = append(cmd.Env, envVars...)

	cloneFlags := uintptr(syscall.CLONE_NEWNS |
		syscall.CLONE_NEWUTS |
		syscall.CLONE_NEWIPC |
		syscall.CLONE_NEWPID)

	// Always create network namespace (we'll configure it if network is enabled)
	cloneFlags |= syscall.CLONE_NEWNET

	// Only use user namespace when NOT running as real root
	// When running as root (e.g., with sudo), we don't need user namespace
	// and it actually prevents setgroups/setuid/setgid from working
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: cloneFlags,
	}

	if os.Geteuid() != 0 {
		// Running as non-root user, need user namespace for isolation
		fmt.Fprintf(os.Stderr, "DEBUG: Running as non-root (euid=%d), using user namespace\n", os.Geteuid())
		cloneFlags |= syscall.CLONE_NEWUSER
		hostUID := os.Getuid()
		hostGID := os.Getgid()

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: cloneFlags,
			UidMappings: []syscall.SysProcIDMap{
				{ContainerID: 0, HostID: hostUID, Size: 65536},
			},
			GidMappings: []syscall.SysProcIDMap{
				{ContainerID: 0, HostID: hostGID, Size: 65536},
			},
		}
	} else {
		fmt.Fprintf(os.Stderr, "DEBUG: Running as root (euid=%d), skipping user namespace\n", os.Geteuid())
	}

	cmd.Env = append(cmd.Env, fmt.Sprintf("PULSE_ROOTFS=%s", rootfs))
	cmd.Env = append(cmd.Env, fmt.Sprintf("PULSE_NETWORK=%v", network))

	// If networking is disabled, just run normally
	if !network {
		err := cmd.Run()
		fixOwnership(rootfs)
		return err
	}

	// For networking enabled, we need to configure after start
	if err := cmd.Start(); err != nil {
		return err
	}

	// Configure network for the container
	containerID := fmt.Sprintf("%d", cmd.Process.Pid)
	if err := ConfigureContainerNetwork(cmd.Process.Pid, containerID); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to configure network: %v", err)
	}

	err := cmd.Wait()
	fixOwnership(rootfs)
	return err
}

// makePathTraversable ensures all parent directories are accessible
func makePathTraversable(path string) {
	// Get all parent directories
	current := path
	dirs := []string{}

	for {
		dirs = append([]string{current}, dirs...)
		parent := filepath.Dir(current)
		if parent == current || parent == "/" {
			break
		}
		current = parent
	}

	// Make each directory traversable (readable + executable)
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}

		// Add o+rx (other read+execute) if not already present
		mode := info.Mode()
		if mode&0005 != 0005 {
			newMode := mode | 0005
			os.Chmod(dir, newMode)
		}
	}
}

// ensureRootOwnership changes ownership of rootfs to root:root when running with sudo
// This is needed because the container runs as real root without user namespace
func ensureRootOwnership(rootfs string) {
	// Recursively chown the rootfs to root
	filepath.Walk(rootfs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		// Change ownership to root (ignore errors for already-owned files)
		os.Lchown(path, 0, 0)
		return nil
	})
}

// fixOwnership changes ownership of rootfs back to the original user when run with sudo
func fixOwnership(rootfs string) {
	sudoUID := os.Getenv("SUDO_UID")
	sudoGID := os.Getenv("SUDO_GID")

	if sudoUID != "" && sudoGID != "" {
		// Running under sudo, fix ownership back to original user
		uid, _ := strconv.Atoi(sudoUID)
		gid, _ := strconv.Atoi(sudoGID)

		// Only fix ownership of files we might have created/modified
		filepath.Walk(rootfs, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors
			}
			// Change ownership (ignore errors for system files)
			os.Lchown(path, uid, gid)
			return nil
		})
	}
}

func ChildProcess(args []string) error {
	rootfs := os.Getenv("PULSE_ROOTFS")
	if rootfs == "" {
		return fmt.Errorf("PULSE_ROOTFS not set")
	}

	// Setup mounts (must be done before chroot)
	if err := setupMounts(rootfs); err != nil {
		return fmt.Errorf("failed to setup mounts: %v", err)
	}

	// Chroot into the rootfs
	if err := syscall.Chroot(rootfs); err != nil {
		return fmt.Errorf("chroot failed: %v", err)
	}

	// Change to root directory after chroot
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir failed: %v", err)
	}

	// Set hostname
	if err := syscall.Sethostname([]byte("container")); err != nil {
		return fmt.Errorf("failed to set hostname: %v", err)
	}

	cmdPath := args[0]

	if filepath.IsAbs(cmdPath) {
		if _, err := os.Stat(cmdPath); err != nil {
			return fmt.Errorf("command not found: %s", cmdPath)
		}
	} else {
		found := false
		searchPaths := []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/usr/local/bin"}

		for _, dir := range searchPaths {
			testPath := filepath.Join(dir, cmdPath)
			if _, err := os.Stat(testPath); err == nil {
				cmdPath = testPath
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("command not found: %s", cmdPath)
		}
	}

	args[0] = cmdPath

	if err := syscall.Exec(cmdPath, args, os.Environ()); err != nil {
		return fmt.Errorf("exec failed for %s: %v", cmdPath, err)
	}

	return nil
}

func setupDNS(rootfs string) error {
	// Copy host's resolv.conf to container
	etcDir := filepath.Join(rootfs, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		return err
	}

	resolvConf := filepath.Join(etcDir, "resolv.conf")

	// Use Google's DNS as default
	dnsContent := "nameserver 8.8.8.8\nnameserver 8.8.4.4\n"

	// Try to read host's resolv.conf and filter out localhost entries
	if hostResolv, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		// Parse and filter nameservers
		var validNameservers []string
		lines := splitLines(string(hostResolv))

		for _, line := range lines {
			trimmed := trimSpace(line)
			// Skip comments and empty lines
			if trimmed == "" || trimmed[0] == '#' {
				continue
			}

			// Check if it's a nameserver line
			if len(trimmed) > 11 && trimmed[:11] == "nameserver " {
				ns := trimSpace(trimmed[11:])
				// Skip localhost addresses (systemd-resolved stub)
				if !isLocalhost(ns) {
					validNameservers = append(validNameservers, ns)
				}
			}
		}

		// If we found valid nameservers, use them
		if len(validNameservers) > 0 {
			dnsContent = ""
			for _, ns := range validNameservers {
				dnsContent += "nameserver " + ns + "\n"
			}
			fmt.Fprintf(os.Stderr, "DEBUG: Using %d nameserver(s) from host\n", len(validNameservers))
		} else {
			fmt.Fprintf(os.Stderr, "DEBUG: No valid nameservers in host config, using public DNS (8.8.8.8, 8.8.4.4)\n")
		}
	} else {
		fmt.Fprintf(os.Stderr, "DEBUG: Cannot read host resolv.conf, using public DNS (8.8.8.8, 8.8.4.4)\n")
	}

	// Remove existing file if it exists (might have wrong permissions)
	os.Remove(resolvConf)

	// Write with appropriate permissions
	if err := os.WriteFile(resolvConf, []byte(dnsContent), 0644); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Wrote DNS config to %s\n", resolvConf)
	return nil
}

func splitLines(s string) []string {
	var lines []string
	var current string
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)

	// Trim leading whitespace
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}

	// Trim trailing whitespace
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}

func isLocalhost(ip string) bool {
	// Check for 127.x.x.x addresses and ::1
	if len(ip) >= 4 && ip[:4] == "127." {
		return true
	}
	if ip == "::1" || ip == "localhost" {
		return true
	}
	return false
}

func setupMounts(rootfs string) error {
	procPath := filepath.Join(rootfs, "proc")
	if err := os.MkdirAll(procPath, 0755); err != nil {
		return err
	}
	if err := syscall.Mount("proc", procPath, "proc", 0, ""); err != nil {
		return fmt.Errorf("failed to mount /proc: %v", err)
	}

	sysPath := filepath.Join(rootfs, "sys")
	if err := os.MkdirAll(sysPath, 0755); err != nil {
		return err
	}
	if err := syscall.Mount("sysfs", sysPath, "sysfs", 0, ""); err != nil {
		return fmt.Errorf("failed to mount /sys: %v", err)
	}

	tmpPath := filepath.Join(rootfs, "tmp")
	if err := os.MkdirAll(tmpPath, 0755); err != nil {
		return err
	}
	if err := syscall.Mount("tmpfs", tmpPath, "tmpfs", 0, ""); err != nil {
		return fmt.Errorf("failed to mount /tmp: %v", err)
	}

	devPath := filepath.Join(rootfs, "dev")
	if err := os.MkdirAll(devPath, 0755); err != nil {
		return err
	}

	devices := []string{"null", "zero", "random", "urandom", "tty"}

	for _, device := range devices {
		hostDev := filepath.Join("/dev", device)
		containerDev := filepath.Join(devPath, device)

		if _, err := os.Stat(hostDev); err != nil {
			continue
		}

		f, err := os.OpenFile(containerDev, os.O_CREATE|os.O_RDONLY, 0666)
		if err != nil {
			continue
		}
		f.Close()

		if err := syscall.Mount(hostDev, containerDev, "", syscall.MS_BIND, ""); err != nil {
			continue
		}
	}

	// Mount /dev/pts for pseudo-terminal support
	devPtsPath := filepath.Join(devPath, "pts")
	if err := os.MkdirAll(devPtsPath, 0755); err == nil {
		// Mount devpts filesystem
		if err := syscall.Mount("devpts", devPtsPath, "devpts", 0, "newinstance,ptmxmode=0666,mode=0620"); err != nil {
			// Non-critical, just log the error
			fmt.Fprintf(os.Stderr, "Warning: failed to mount /dev/pts: %v\n", err)
		}
	}

	return nil
}
