i.MX Ethernet driver
====================

This Go package implements TCP/IP connectivity through Ethernet (ENET) on NXP
i.MX SoCs, to be used with `GOOS=tamago GOARCH=arm` as supported by the
[TamaGo](https://github.com/usbarmory/tamago) framework for bare metal Go.

The package supports TCP/IP networking through gVisor (`go` branch)
[tcpip](https://pkg.go.dev/gvisor.dev/gvisor/pkg/tcpip)
stack pure Go implementation.

The interface TCP/IP stack can be attached to the Go runtime by setting
`net.SocketFunc` to the interface `Socket` function:

```
// i.MX ENET interface
iface := imxenet.Interface{}

// initialize IP, Netmask, MAC, Gateway
_ = imxenet.Init(usbarmory.ENET2, "10.0.0.1", "255.255.255.0", "1a:55:89:a2:69:41", "10.0.0.2")

// Go runtime hook
net.SocketFunc = iface.Socket
```

See [tamago-example](https://github.com/usbarmory/tamago-example/blob/master/network/imx-enet.go)
for a full integration example.

Authors
=======

Andrea Barisani  
andrea@inversepath.com  

Andrej Rosano  
andrej@inversepath.com  

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
Copyright (c) The imx-enet authors. All Rights Reserved.

These source files are distributed under the BSD-style license found in the
[LICENSE](https://github.com/usbarmory/imx-enet/blob/master/LICENSE) file.
