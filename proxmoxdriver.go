package dockermachinedriverproxmoxve

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/resty.v1"

	sshrw "github.com/mosolovsa/go_cat_sshfilerw"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/state"
	"github.com/labstack/gommon/log"
)

// Driver for Proxmox VE
type Driver struct {
	*drivers.BaseDriver
	driver *ProxmoxVE

	// Basic Authentication for Proxmox VE
	Host     string // Host to connect to
	Node     string // optional, node to create VM on, host used if omitted but must match internal node name
	User     string // username
	Password string // password
	Realm    string // realm, e.g. pam, pve, etc.

	// File to load as boot image RancherOS/Boot2Docker
	ImageFile string // in the format <storagename>:iso/<filename>.iso

	Pool            string // pool to add the VM to (necessary for users with only pool permission)
	Storage         string // internal PVE storage name
	StorageType     string // Type of the storage (currently QCOW2 and RAW)
	DiskSize        string // disk size in GB
	Memory          int    // memory in GB
	StorageFilename string

	VMID          string // VM ID only filled by create()
	GuestUsername string // user to log into the guest OS to copy the public key
	GuestPassword string // password to log into the guest OS to copy the public key
	GuestSSHPort  int    // ssh port to log into the guest OS to copy the public key

	driverDebug bool // driver debugging
	restyDebug  bool // enable resty debugging
}

func (d *Driver) debugf(format string, v ...interface{}) {
	if d.driverDebug {
		log.Infof(format, v...)
	}
}

func (d *Driver) debug(v ...interface{}) {
	if d.driverDebug {
		log.Info(v...)
	}
}

func (d *Driver) connectAPI() error {
	if d.driver == nil {
		d.debugf("Create called")

		d.debugf("Connecting to %s as %s@%s with password '%s'", d.Host, d.User, d.Realm, d.Password)
		c, err := GetProxmoxVEConnectionByValues(d.User, d.Password, d.Realm, d.Host)
		d.driver = c
		if err != nil {
			return fmt.Errorf("Could not connect to host '%s' with '%s@%s'", d.Host, d.User, d.Realm)
		}
		if d.restyDebug {
			c.EnableDebugging()
		}
		d.debugf("Connected to PVE version '" + d.driver.Version + "'")
	}
	return nil
}

// GetCreateFlags returns the argument flags for the program
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_HOST",
			Name:   "proxmoxve-host",
			Usage:  "Host to connect to",
			Value:  "192.168.1.253",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_DISKSIZE_GB",
			Name:   "proxmoxve-disksize-gb",
			Usage:  "disk size in GB",
			Value:  "16",
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_MEMORY_GB",
			Name:   "proxmoxve-memory-gb",
			Usage:  "memory in GB",
			Value:  8,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_STORAGE",
			Name:   "proxmoxve-storage",
			Usage:  "storage to create the VM volume on",
			Value:  "local",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_NODE",
			Name:   "proxmoxve-node",
			Usage:  "to to use (defaults to host)",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_USER",
			Name:   "proxmoxve-user",
			Usage:  "User to connect as",
			Value:  "root",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_REALM",
			Name:   "proxmoxve-realm",
			Usage:  "Realm to connect to (default: pam)",
			Value:  "pam",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PASSWORD",
			Name:   "proxmoxve-password",
			Usage:  "Password to connect with",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_IMAGE_FILE",
			Name:   "proxmoxve-image-file",
			Usage:  "storage of the image file (e.g. local:iso/rancheros-proxmoxve-autoformat.iso)",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_POOL",
			Name:   "proxmoxve-pool",
			Usage:  "pool to attach to",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_STORAGE_TYPE",
			Name:   "proxmoxve-storage-type",
			Usage:  "storage type to use (QCOW2 or RAW)",
			Value:  "raw",
		},
		mcnflag.StringFlag{
			Name:  "proxmoxve-guest-username",
			Usage: "Username to log in to the guest OS (default docker for rancheros)",
			Value: "docker",
		},
		mcnflag.StringFlag{
			Name:  "proxmoxve-guest-password",
			Usage: "Password to log in to the guest OS (default tcuser for rancheros)",
			Value: "tcuser",
		},
		mcnflag.IntFlag{
			Name:  "proxmoxve-guest-ssh-port",
			Usage: "SSH port in the guest to log in to (defaults to 22)",
			Value: 22,
		},
		mcnflag.BoolFlag{
			Name:  "proxmoxve-resty-debug",
			Usage: "enables the resty debugging",
		},
		mcnflag.BoolFlag{
			Name:  "proxmoxve-driver-debug",
			Usage: "enables debugging in the driver",
		},
	}
}

func (d *Driver) ping() bool {
	if d.driver == nil {
		return false
	}

	command := NodesNodeQemuVMIDAgentPostParameter{Command: "ping"}
	err := d.driver.NodesNodeQemuVMIDAgentPost(d.Node, d.VMID, &command)

	if err != nil {
		d.debug(err)
		return false
	}

	return true
}

// DriverName returns the name of the driver
func (d *Driver) DriverName() string {
	return "proxmoxve"
}

// SetConfigFromFlags configures all command line arguments
func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.debug("SetConfigFromFlags called")
	d.ImageFile = flags.String("proxmoxve-image-file")
	d.Host = flags.String("proxmoxve-host")
	d.Node = flags.String("proxmoxve-node")
	if len(d.Node) == 0 {
		d.Node = d.Host
	}
	d.User = flags.String("proxmoxve-user")
	d.Realm = flags.String("proxmoxve-realm")
	d.Pool = flags.String("proxmoxve-pool")
	d.Password = flags.String("proxmoxve-password")
	d.DiskSize = flags.String("proxmoxve-disksize-gb")
	d.Storage = flags.String("proxmoxve-storage")
	d.StorageType = strings.ToLower(flags.String("proxmoxve-storage-type"))
	d.Memory = flags.Int("proxmoxve-memory-gb")
	d.Memory *= 1024

	d.SwarmMaster = flags.Bool("swarm-master")
	d.SwarmHost = flags.String("swarm-host")
	d.GuestSSHPort = flags.Int("proxmoxve-guest-ssh-port")
	d.GuestUsername = flags.String("proxmoxve-guest-username")
	d.GuestPassword = flags.String("proxmoxve-guest-password")

	d.driverDebug = flags.Bool("proxmoxve-driver-debug")
	d.restyDebug = flags.Bool("proxmoxve-resty-debug")

	if d.restyDebug {
		d.debug("enabling Resty debugging")
		resty.SetLogger(log.Output())
		resty.SetDebug(true)
	}

	return nil
}

// GetURL returns the URL for the target docker daemon
func (d *Driver) GetURL() (string, error) {
	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}
	if ip == "" {
		return "", nil
	}
	return fmt.Sprintf("tcp://%s:2376", ip), nil
}

// GetMachineName returns the machine name
func (d *Driver) GetMachineName() string {
	return d.MachineName
}

// GetIP returns the ip
func (d *Driver) GetIP() (string, error) {
	d.connectAPI()
	return d.driver.GetEth0IPv4(d.Node, d.VMID)
}

// GetSSHHostname returns the ssh host returned by the API
func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// GetSSHPort returns the ssh port, 22 if not specified
func (d *Driver) GetSSHPort() (int, error) {
	return d.GuestSSHPort, nil
}

// GetSSHUsername returns the ssh user name, root if not specified
func (d *Driver) GetSSHUsername() string {
	return d.GuestUsername
}

// GetState returns the state of the VM
func (d *Driver) GetState() (state.State, error) {
	err := d.connectAPI()
	if err != nil {
		return state.Paused, err
	}

	if d.ping() {
		return state.Running, nil
	}
	return state.Paused, nil
}

// PreCreateCheck is called to enforce pre-creation steps
func (d *Driver) PreCreateCheck() error {

	switch d.StorageType {
	case "raw":
		fallthrough
	case "qcow2":
		break
	default:
		return fmt.Errorf("storage type '%s' is not supported", d.StorageType)
	}

	err := d.connectAPI()
	if err != nil {
		return err
	}

	d.debug("Retrieving next ID")
	id, err := d.driver.ClusterNextIDGet(0)
	if err != nil {
		return err
	}
	d.debugf("Next ID was '%s'", id)
	d.VMID = id

	storageType, err := d.driver.GetStorageType(d.Node, d.Storage)
	if err != nil {
		return err
	}

	filename := "vm-" + d.VMID + "-disk-1"
	switch storageType {
	case "lvmthin":
		fallthrough
	case "zfs":
		fallthrough
	case "ceph":
		if d.StorageType != "raw" {
			return fmt.Errorf("type '%s' on storage '%s' does only support raw", storageType, d.Storage)
		}
	default:
		filename += "." + d.StorageType
	}
	d.StorageFilename = filename

	// create and save a new SSH key pair
	keyfile := d.GetSSHKeyPath()
	keypath := path.Dir(keyfile)
	d.debugf("Generating new key pair at path '%s'", keypath)
	err = os.MkdirAll(keypath, 0755)
	if err != nil {
		return err
	}
	_, _, err = GetKeyPair(keyfile)

	return err
}

// Create creates a new VM with storage
func (d *Driver) Create() error {

	volume := NodesNodeStorageStorageContentPostParameter{
		Filename: d.StorageFilename,
		Size:     d.DiskSize + "G",
		VMID:     d.VMID,
	}

	d.debugf("Creating disk volume '%s' with size '%s'", volume.Filename, volume.Size)
	diskname, err := d.driver.NodesNodeStorageStorageContentPost(d.Node, d.Storage, &volume)
	if err != nil {
		return err
	}

	if !strings.HasSuffix(diskname, d.StorageFilename) {
		return fmt.Errorf("returned diskname is not correct: should be '%s' but was '%s'", d.StorageFilename, diskname)
	}

	npp := NodesNodeQemuPostParameter{
		VMID:      d.VMID,
		Agent:     "1",
		Autostart: "1",
		Memory:    d.Memory,
		Cores:     "4",
		Net0:      "virtio,bridge=vmbr0",
		SCSI0:     d.StorageFilename,
		Ostype:    "l26",
		Name:      d.BaseDriver.MachineName,
		KVM:       "1", // if you test in a nested environment, you may have to change this to 0 if you do not have nested virtualization
		Cdrom:     d.ImageFile,
		Pool:      d.Pool,
	}

	if d.StorageType == "qcow2" {
		npp.SCSI0 = d.Storage + ":" + d.VMID + "/" + volume.Filename
	}
	d.debugf("Creating VM '%s' with '%d' of memory", npp.VMID, npp.Memory)
	taskid, err := d.driver.NodesNodeQemuPost(d.Node, &npp)
	if err != nil {
		return err
	}

	err = d.driver.WaitForTaskToComplete(d.Node, taskid)
	if err != nil {
		return err
	}

	err = d.Start()
	if err != nil {
		return err
	}

	return d.waitAndPrepareSSH()
}
func (d *Driver) waitAndPrepareSSH() error {
	d.debugf("waiting for VM to become active, first wait 10 seconds")
	time.Sleep(10 * time.Second)

	for !d.ping() {
		d.debugf("waiting for VM to become active")
		time.Sleep(2 * time.Second)
	}
	d.debugf("VM is active waiting more")
	time.Sleep(2 * time.Second)

	sshConfig := &ssh.ClientConfig{
		User: d.GetSSHUsername(),
		Auth: []ssh.AuthMethod{
			ssh.Password(d.GuestPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshbasedir := "/home/" + d.GetSSHUsername() + "/.ssh"
	hostname, _ := d.GetSSHHostname()
	port, _ := d.GetSSHPort()
	clientstr := fmt.Sprintf("%s:%d", hostname, port)

	d.debugf("Creating directory '%s'", sshbasedir)
	conn, err := ssh.Dial("tcp", clientstr, sshConfig)
	if err != nil {
		return err
	}
	session, err := conn.NewSession()
	if err != nil {
		return err
	}

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Run("mkdir -p " + sshbasedir)
	d.debugf(fmt.Sprintf("%s -> %s", hostname, stdoutBuf.String()))
	session.Close()

	d.debugf("Trying to copy to %s:%s", clientstr, sshbasedir)
	c, err := sshrw.NewSSHclt(clientstr, sshConfig)
	if err != nil {
		return err
	}

	// Open a file
	f, err := os.Open(d.GetSSHKeyPath() + ".pub")
	if err != nil {
		return err
	}

	// TODO: always fails with return status 127, but file was copied correclty
	c.WriteFile(f, sshbasedir+"/authorized_keys")
	// if err = c.WriteFile(f, sshbasedir+"/authorized_keys"); err != nil {
	// 	d.debugf("Error on file write: ", err)
	// }

	// Close the file after it has been copied
	defer f.Close()

	return err
}

// Start starts the VM
func (d *Driver) Start() error {
	err := d.connectAPI()
	if err != nil {
		return err
	}
	taskid, err := d.driver.NodesNodeQemuVMIDStatusStartPost(d.Node, d.VMID)

	if err != nil {
		return err
	}

	err = d.driver.WaitForTaskToComplete(d.Node, taskid)

	return err
}

// Stop stopps the VM
func (d *Driver) Stop() error {
	return nil
}

// Restart restarts the VM
func (d *Driver) Restart() error {
	d.Stop()
	d.Start()
	return nil
}

// Kill is currently a NOOP
func (d *Driver) Kill() error {
	return nil
}

// Remove removes the VM
func (d *Driver) Remove() error {
	err := d.connectAPI()
	if err != nil {
		return err
	}
	taskid, err := d.driver.NodesNodeQemuVMIDDelete(d.Node, d.VMID)

	if err != nil {
		return err
	}

	err = d.driver.WaitForTaskToComplete(d.Node, taskid)
	return err
}

// Upgrade is currently a NOOP
func (d *Driver) Upgrade() error {
	return nil
}

// NewDriver returns a new driver
func NewDriver(hostName, storePath string) drivers.Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			SSHUser:     "docker",
			MachineName: hostName,
			StorePath:   storePath,
		},
	}
}

// GetKeyPair returns a public/private key pair and an optional error
func GetKeyPair(file string) (string, string, error) {
	// read keys from file
	_, err := os.Stat(file)
	if err == nil {
		priv, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Printf("Failed to read file - %s", err)
			goto genKeys
		}
		pub, err := ioutil.ReadFile(file + ".pub")
		if err != nil {
			fmt.Printf("Failed to read pub file - %s", err)
			goto genKeys
		}
		return string(pub), string(priv), nil
	}

	// generate keys and save to file
genKeys:
	pub, priv, err := GenKeyPair()
	err = ioutil.WriteFile(file, []byte(priv), 0600)
	if err != nil {
		return "", "", fmt.Errorf("Failed to write file - %s", err)
	}
	err = ioutil.WriteFile(file+".pub", []byte(pub), 0644)
	if err != nil {
		return "", "", fmt.Errorf("Failed to write pub file - %s", err)
	}

	return pub, priv, nil
}

// GenKeyPair returns a freshly created public/private key pair and an optional error
func GenKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	var private bytes.Buffer
	if err := pem.Encode(&private, privateKeyPEM); err != nil {
		return "", "", err
	}

	// generate public key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}

	public := ssh.MarshalAuthorizedKey(pub)
	return string(public), private.String(), nil
}
