i.MX Ethernet driver
====================

This Go package implements TCP/IP connectivity through Ethernet (ENET) on NXP
i.MX SoCs, to be used with `GOOS=tamago GOARCH=arm` as supported by the
[TamaGo](https://github.com/usbarmory/tamago) framework for bare metal Go on
ARM SoCs.

The package supports TCP/IP networking through gVisor [tcpip](https://pkg.go.dev/gvisor.dev/gvisor/pkg/tcpip)
package (`go` branch) stack pure Go implementation.

Authors
=======

Andrea Barisani  
andrea.barisani@withsecure.com | andrea@inversepath.com  

Andrej Rosano  
andrej.rosano@withsecure.com   | andrej@inversepath.com  

Documentation
=============

The package API documentation can be found on
[pkg.go.dev](https://pkg.go.dev/github.com/usbarmory/imx-enet).


For more information about TamaGo see its
[repository](https://github.com/usbarmory/tamago) and
[project wiki](https://github.com/usbarmory/tamago/wiki).

License
=======

tamago | https://github.com/usbarmory/imx-enet  
Copyright (c) WithSecure Corporation

These source files are distributed under the BSD-style license found in the
[LICENSE](https://github.com/usbarmory/imx-enet/blob/master/LICENSE) file.
