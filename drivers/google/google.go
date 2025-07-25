package google

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/rancher/machine/libmachine/drivers"
	rpcdriver "github.com/rancher/machine/libmachine/drivers/rpc"
	"github.com/rancher/machine/libmachine/log"
	"github.com/rancher/machine/libmachine/mcnflag"
	"github.com/rancher/machine/libmachine/ssh"
	"github.com/rancher/machine/libmachine/state"
)

// Driver is a struct compatible with the docker.hosts.drivers.Driver interface.
type Driver struct {
	*drivers.BaseDriver
	Auth                       string
	Zone                       string
	MachineType                string
	MachineImage               string
	DiskType                   string
	Address                    string
	Network                    string
	Subnetwork                 string
	Preemptible                bool
	UseInternalIP              bool
	UseInternalIPOnly          bool
	Scopes                     string
	DiskSize                   int
	Project                    string
	Tags                       string
	Labels                     string
	UseExisting                bool
	OpenPorts                  []string
	ExternalFirewallRulePrefix string
	InternalFirewallRulePrefix string
	Userdata                   string
}

const (
	defaultZone        = "us-central1-a"
	defaultUser        = "docker-user"
	defaultMachineType = "n1-standard-1"
	defaultImageName   = "ubuntu-os-cloud/global/images/ubuntu-2204-jammy-v20220420"
	defaultScopes      = "https://www.googleapis.com/auth/devstorage.read_only,https://www.googleapis.com/auth/logging.write,https://www.googleapis.com/auth/monitoring.write"
	defaultDiskType    = "pd-standard"
	defaultDiskSize    = 10
	defaultNetwork     = "default"
	defaultSubnetwork  = ""
)

// GetCreateFlags registers the flags this driver adds to
// "docker hosts create"
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			Name:   "google-zone",
			Usage:  "GCE Zone",
			Value:  defaultZone,
			EnvVar: "GOOGLE_ZONE",
		},
		mcnflag.StringFlag{
			Name:   "google-machine-type",
			Usage:  "GCE Machine Type",
			Value:  defaultMachineType,
			EnvVar: "GOOGLE_MACHINE_TYPE",
		},
		mcnflag.StringFlag{
			Name:   "google-machine-image",
			Usage:  "GCE Machine Image Absolute URL",
			Value:  defaultImageName,
			EnvVar: "GOOGLE_MACHINE_IMAGE",
		},
		mcnflag.StringFlag{
			Name:   "google-username",
			Usage:  "GCE User Name",
			Value:  defaultUser,
			EnvVar: "GOOGLE_USERNAME",
		},
		mcnflag.StringFlag{
			Name:   "google-auth-encoded-json",
			Usage:  "Base64 encoded GCE auth json",
			EnvVar: "GOOGLE_AUTH_ENCODED_JSON",
		},
		mcnflag.StringFlag{
			Name:   "google-project",
			Usage:  "GCE Project",
			EnvVar: "GOOGLE_PROJECT",
		},
		mcnflag.StringFlag{
			Name:   "google-scopes",
			Usage:  "GCE Scopes (comma-separated if multiple scopes)",
			Value:  defaultScopes,
			EnvVar: "GOOGLE_SCOPES",
		},
		mcnflag.IntFlag{
			Name:   "google-disk-size",
			Usage:  "GCE Instance Disk Size (in GB)",
			Value:  defaultDiskSize,
			EnvVar: "GOOGLE_DISK_SIZE",
		},
		mcnflag.StringFlag{
			Name:   "google-disk-type",
			Usage:  "GCE Instance Disk type",
			Value:  defaultDiskType,
			EnvVar: "GOOGLE_DISK_TYPE",
		},
		mcnflag.StringFlag{
			Name:   "google-network",
			Usage:  "Specify network in which to provision vm",
			Value:  defaultNetwork,
			EnvVar: "GOOGLE_NETWORK",
		},
		mcnflag.StringFlag{
			Name:   "google-subnetwork",
			Usage:  "Specify subnetwork in which to provision vm",
			Value:  defaultSubnetwork,
			EnvVar: "GOOGLE_SUBNETWORK",
		},
		mcnflag.StringFlag{
			Name:   "google-address",
			Usage:  "GCE Instance External IP",
			EnvVar: "GOOGLE_ADDRESS",
		},
		mcnflag.BoolFlag{
			Name:   "google-preemptible",
			Usage:  "GCE Instance Preemptibility",
			EnvVar: "GOOGLE_PREEMPTIBLE",
		},
		mcnflag.StringFlag{
			Name:   "google-tags",
			Usage:  "GCE Instance Tags (comma-separated)",
			EnvVar: "GOOGLE_TAGS",
			Value:  "",
		},
		mcnflag.BoolFlag{
			Name:   "google-use-internal-ip",
			Usage:  "Use internal GCE Instance IP rather than public one",
			EnvVar: "GOOGLE_USE_INTERNAL_IP",
		},
		mcnflag.BoolFlag{
			Name:   "google-use-internal-ip-only",
			Usage:  "Configure GCE instance to not have an external IP address",
			EnvVar: "GOOGLE_USE_INTERNAL_IP_ONLY",
		},
		mcnflag.BoolFlag{
			Name:   "google-use-existing",
			Usage:  "Don't create a new VM, use an existing one",
			EnvVar: "GOOGLE_USE_EXISTING",
		},
		mcnflag.StringSliceFlag{
			Name:   "google-open-port",
			Usage:  "Make the specified port number accessible from the Internet, e.g, 8080/tcp",
			EnvVar: "GOOGLE_OPEN_PORT",
		},
		mcnflag.StringFlag{
			Name:   "google-external-firewall-rule-prefix",
			Usage:  "A prefix to be added to the fire wall rule created when opening ports publicly to ensure uniqueness",
			EnvVar: "GOOGLE_EXTERNAL_FIREWALL_RULE_PREFIX",
		},
		mcnflag.StringFlag{
			Name:   "google-internal-firewall-rule-prefix",
			Usage:  "A prefix to be added to the firewall rule created when exposing ports internally to ensure uniqueness",
			EnvVar: "GOOGLE_INTERNAL_FIREWALL_RULE_PREFIX",
		},
		mcnflag.StringFlag{
			Name:   "google-userdata",
			Usage:  "A user-data file to be passed to cloud-init",
			EnvVar: "GOOGLE_USERDATA",
			Value:  "",
		},
		mcnflag.StringFlag{
			Name:   "google-vm-labels",
			Usage:  "labels to add onto the created virtual machine",
			EnvVar: "GOOGLE_VM_LABELS",
			Value:  "",
		},
	}
}

// NewDriver creates a Driver with the specified storePath.
func NewDriver(machineName string, storePath string) *Driver {
	return &Driver{
		Zone:         defaultZone,
		DiskType:     defaultDiskType,
		DiskSize:     defaultDiskSize,
		MachineType:  defaultMachineType,
		MachineImage: defaultImageName,
		Network:      defaultNetwork,
		Subnetwork:   defaultSubnetwork,
		Scopes:       defaultScopes,
		BaseDriver: &drivers.BaseDriver{
			SSHUser:     defaultUser,
			MachineName: machineName,
			StorePath:   storePath,
		},
	}
}

// GetSSHHostname returns hostname for use with ssh
func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// GetSSHUsername returns username for use with ssh
func (d *Driver) GetSSHUsername() string {
	if d.SSHUser == "" {
		d.SSHUser = "docker-user"
	}
	return d.SSHUser
}

// DriverName returns the name of the driver
func (d *Driver) DriverName() string {
	return "google"
}

// UnmarshalJSON loads driver config from JSON. This function is used by the RPCServerDriver that wraps
// all drivers as a means of populating an already-initialized driver with new configuration.
// See `RPCServerDriver.SetConfigRaw`.
func (d *Driver) UnmarshalJSON(data []byte) error {
	// Unmarshal driver config into an aliased type to prevent infinite recursion on UnmarshalJSON.
	type targetDriver Driver

	// Copy data from `d` to `target` before unmarshalling. This will ensure that already-initialized values
	// from `d` that are left untouched during unmarshal (like functions) are preserved.
	target := targetDriver(*d)

	if err := json.Unmarshal(data, &target); err != nil {
		return fmt.Errorf("error unmarshalling driver config from JSON: %w", err)
	}

	// Copy unmarshalled data back to `d`.
	*d = Driver(target)

	// Make sure to reload values that are subject to change from envvars and os.Args.
	driverOpts := rpcdriver.GetDriverOpts(d.GetCreateFlags(), os.Args)
	if _, ok := driverOpts.Values["google-auth-encoded-json"]; ok {
		d.Auth = driverOpts.String("google-auth-encoded-json")
	}

	return nil
}

// SetConfigFromFlags initializes the driver based on the command line flags.
func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.Project = flags.String("google-project")
	if d.Project == "" {
		return errors.New("no Google Cloud Project name specified (--google-project)")
	}
	d.Auth = flags.String("google-auth-encoded-json")

	d.Zone = flags.String("google-zone")
	d.UseExisting = flags.Bool("google-use-existing")
	if !d.UseExisting {
		d.MachineType = flags.String("google-machine-type")
		d.MachineImage = flags.String("google-machine-image")
		d.MachineImage = strings.TrimPrefix(d.MachineImage, "https://www.googleapis.com/compute/v1/projects/")
		d.DiskSize = flags.Int("google-disk-size")
		d.DiskType = flags.String("google-disk-type")
		d.Address = flags.String("google-address")
		d.Network = flags.String("google-network")
		d.Subnetwork = flags.String("google-subnetwork")
		d.Preemptible = flags.Bool("google-preemptible")
		d.UseInternalIP = flags.Bool("google-use-internal-ip") || flags.Bool("google-use-internal-ip-only")
		d.UseInternalIPOnly = flags.Bool("google-use-internal-ip-only")
		d.Scopes = flags.String("google-scopes")
		d.Tags = flags.String("google-tags")
		d.OpenPorts = flags.StringSlice("google-open-port")
		d.ExternalFirewallRulePrefix = flags.String("google-external-firewall-rule-prefix")
		d.InternalFirewallRulePrefix = flags.String("google-internal-firewall-rule-prefix")
		d.Labels = flags.String("google-vm-labels")
	}
	d.SSHUser = flags.String("google-username")
	d.SSHPort = 22
	d.Userdata = flags.String("google-userdata")
	d.SetSwarmConfigFromFlags(flags)

	return nil
}

// PreCreateCheck is called to enforce pre-creation steps
func (d *Driver) PreCreateCheck() error {
	c, err := newComputeUtil(d)
	if err != nil {
		return err
	}

	// Check that the project exists. It will also check the credentials
	// at the same time.
	log.Infof("Check that the project exists")

	if _, err = c.service.Projects.Get(d.Project).Do(); err != nil {
		return fmt.Errorf("Project with ID %q not found. %v", d.Project, err)
	}

	// Check if the instance already exists. There will be an error if the instance
	// doesn't exist, so just check instance for nil.
	log.Infof("Check if the instance already exists")

	instance, _ := c.instance()
	if d.UseExisting {
		if instance == nil {
			return fmt.Errorf("unable to find instance %q in zone %q", d.MachineName, d.Zone)
		}
	} else {
		if instance != nil {
			return fmt.Errorf("instance %q already exists in zone %q", d.MachineName, d.Zone)
		}
	}

	if d.Userdata != "" {
		file, err := os.ReadFile(d.Userdata)
		if err != nil {
			return fmt.Errorf("cannot read userdata file %v: %v", d.Userdata, err)
		}
		d.Userdata = string(file)
	}

	return nil
}

// Create creates a GCE VM instance acting as a docker host.
func (d *Driver) Create() error {
	log.Infof("Generating SSH Key")

	if err := ssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
		return err
	}

	log.Infof("Creating host...")

	c, err := newComputeUtil(d)
	if err != nil {
		return err
	}

	if len(d.OpenPorts) > 0 {
		if d.ExternalFirewallRulePrefix == "" {
			return fmt.Errorf("the 'google-external-firewall-rule-prefix' flag must be provided when opening ports publicly")
		}
		if err := c.openPublicFirewallPorts(d); err != nil {
			return err
		}
	}

	if d.InternalFirewallRulePrefix != "" {
		if err := c.openInternalFirewallPorts(d); err != nil {
			return err
		}
	} else {
		log.Debugf("Not creating internal firewall rule as no prefix has been provided")
	}

	if d.UseExisting {
		return c.configureInstance(d)
	}
	return c.createInstance(d)
}

// GetURL returns the URL of the remote docker daemon.
func (d *Driver) GetURL() (string, error) {
	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("tcp://%s", net.JoinHostPort(ip, "2376")), nil
}

// GetIP returns the IP address of the GCE instance.
func (d *Driver) GetIP() (string, error) {
	c, err := newComputeUtil(d)
	if err != nil {
		return "", err
	}

	ip, err := c.ip()
	if err != nil {
		return "", err
	}
	if ip == "" {
		return "", drivers.ErrHostIsNotRunning
	}

	return ip, nil
}

// GetState returns a docker.hosts.state.State value representing the current state of the host.
func (d *Driver) GetState() (state.State, error) {
	c, err := newComputeUtil(d)
	if err != nil {
		return state.None, err
	}

	// All we care about is whether the disk exists, so we just check disk for a nil value.
	// There will be no error if disk is not nil.
	instance, err := c.instance()
	if instance == nil {
		if err != nil && strings.Contains(err.Error(), "not found") {
			return state.NotFound, nil
		}
		disk, _ := c.disk()
		if disk == nil {
			return state.None, nil
		}
		return state.Stopped, nil
	}

	switch instance.Status {
	case "PROVISIONING", "STAGING":
		return state.Starting, nil
	case "RUNNING":
		return state.Running, nil
	case "STOPPING", "STOPPED", "TERMINATED":
		return state.Stopped, nil
	}
	return state.None, nil
}

// Start starts an existing GCE instance or create an instance with an existing disk.
func (d *Driver) Start() error {
	c, err := newComputeUtil(d)
	if err != nil {
		return err
	}

	instance, err := c.instance()
	if err != nil {
		if !isNotFound(err) {
			return err
		}
	}

	if instance == nil {
		if err = c.createInstance(d); err != nil {
			return err
		}
	} else {
		if err := c.startInstance(); err != nil {
			return err
		}
	}

	d.IPAddress, err = d.GetIP()
	return err
}

// Stop stops an existing GCE instance.
func (d *Driver) Stop() error {
	c, err := newComputeUtil(d)
	if err != nil {
		return err
	}

	if err := c.stopInstance(); err != nil {
		return err
	}

	d.IPAddress = ""
	return nil
}

// Restart restarts a machine which is known to be running.
func (d *Driver) Restart() error {
	if err := d.Stop(); err != nil {
		return err
	}

	return d.Start()
}

// Kill stops an existing GCE instance.
func (d *Driver) Kill() error {
	return d.Stop()
}

// Remove deletes the GCE instance and the disk.
func (d *Driver) Remove() error {
	c, err := newComputeUtil(d)
	if err != nil {
		return err
	}

	if err := c.deleteInstance(); err != nil {
		if isNotFound(err) {
			log.Warn("Remote instance does not exist, proceeding with removing local reference")
		} else {
			return err
		}
	}

	if err := c.deleteDisk(); err != nil {
		if isNotFound(err) {
			log.Warn("Remote disk does not exist, proceeding")
		} else {
			return err
		}
	}

	// collect all errors and only return them
	// later. If we fail to destroy one firewall,
	// we should still attempt to remove the other.
	var errs []error
	if len(d.OpenPorts) > 0 {
		externalFwRule, err := c.externalFirewallRule()
		if isNotFound(err) {
			log.Infof("external firewall rule '%s' does not exist, nothing to do", c.externalFirewallRuleName())
		} else if err != nil {
			log.Warnf("failed to get external firewall rule '%s' while deleting VM: %v", c.externalFirewallRuleName(), err)
			errs = append(errs, err)
		} else if err = c.CleanUpFirewallRule(externalFwRule, externalFirewallRuleLabelKey); err != nil {
			log.Errorf("failed remove external firewall rule '%s': %v", c.externalFirewallRuleName(), err)
			errs = append(errs, err)
		}
	}

	if c.internalFirewallRulePrefix != "" {
		internalFwRule, err := c.internalFirewallRule()
		if isNotFound(err) {
			log.Infof("internal firewall rule '%s' does not exist, nothing to do", c.internalFirewallRuleName())
		} else if err != nil {
			log.Warnf("failed to get internal firewall rule '%s' while deleting VM: %v", c.internalFirewallRuleName(), err)
			errs = append(errs, err)
		} else if err = c.CleanUpFirewallRule(internalFwRule, internalFirewallRuleLabelKey); err != nil {
			log.Errorf("failed remove internal firewall rule '%s': %v", c.internalFirewallRuleName(), err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
