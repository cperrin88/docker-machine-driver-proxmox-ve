module github.com/cperrin88/docker-machine-driver-proxmox-ve

go 1.13

require (
	github.com/Azure/go-ansiterm v0.0.0-20170929234023-d6e3b3328b78 // indirect
	github.com/docker/docker v1.13.1
	github.com/docker/machine v0.16.2
	github.com/labstack/gommon v0.3.0
	github.com/lnxbil/docker-machine-driver-proxmox-ve v0.0.0-20191205133416-3379f3bc95cd
	github.com/mosolovsa/go_cat_sshfilerw v0.0.0-20170307095915-3f6912dbd9e4
	github.com/sirupsen/logrus v1.4.2 // indirect
	golang.org/x/crypto v0.0.0-20200115085410-6d4e4cb37c7d
	gopkg.in/resty.v1 v1.12.0
)

replace github.com/docker/docker => github.com/docker/engine v1.4.2-0.20191113042239-ea84732a7725
