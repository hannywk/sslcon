package utils

import (
    "fmt"
    "golang.org/x/sys/windows"
    "golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
    "net"
    "net/netip"
    "os/exec"
    "strings"
    "vpnagent/base"
    "vpnagent/tun"
)

var localInterface winipcfg.LUID

func ConfigInterface(TunName, VPNAddress, VPNMask, ServerIP string, DNS, SplitInclude, SplitExclude []string) error {
    mtu, _ := tun.NativeTunDevice.MTU()
    err := SetMTU(TunName, mtu)
    if err != nil {
        return err
    }

    iface := winipcfg.LUID(tun.NativeTunDevice.LUID())

    // ip address
    iface.FlushIPAddresses(windows.AF_UNSPEC)

    prefixVPN, _ := netip.ParsePrefix(IpMask2CIDR(VPNAddress, VPNMask))
    err = iface.SetIPAddressesForFamily(windows.AF_INET, []netip.Prefix{prefixVPN})
    if err != nil {
        return err
    }

    // routes
    nextHopVPN, _ := netip.ParseAddr(VPNAddress)
    targetServer, _ := netip.ParsePrefix(ServerIP + "/32")
    nextHopVPNGateway, _ := netip.ParseAddr(base.LocalInterface.Gateway)
    localInterface.AddRoute(targetServer, nextHopVPNGateway, 6)

    if len(SplitInclude) == 0 {
        targetDefault, _ := netip.ParsePrefix("0.0.0.0/0")
        err = iface.AddRoute(targetDefault, nextHopVPN, 5)
        if err != nil {
            return err
        }
        if len(SplitExclude) != 0 {
            routes := []*winipcfg.RouteData{}
            for _, ipMask := range SplitExclude {
                prefixExclude, _ := netip.ParsePrefix(IpMaskToCIDR(ipMask))
                routes = append(routes, &winipcfg.RouteData{prefixExclude, nextHopVPNGateway, 6})
            }
            localInterface.AddRoutes(routes)
        }
    } else {
        routes := []*winipcfg.RouteData{}
        for _, ipMask := range SplitInclude {
            prefixInclude, _ := netip.ParsePrefix(IpMaskToCIDR(ipMask))
            routes = append(routes, &winipcfg.RouteData{prefixInclude, nextHopVPN, 5})
        }
        iface.AddRoutes(routes)
    }

    // dns
    var servers []netip.Addr
    for _, dns := range DNS {
        addr, _ := netip.ParseAddr(dns)
        servers = append(servers, addr)
    }

    err = iface.SetDNS(windows.AF_INET, servers, []string{})
    if err != nil {
        return err
    }

    return nil
}

func ResetRouting(ServerIP string, DNS, SplitExclude []string) {
    localInterface.FlushRoutes(windows.AF_INET)

    targetDefault, _ := netip.ParsePrefix("0.0.0.0/0")
    nextHopVPNGateway, _ := netip.ParseAddr(base.LocalInterface.Gateway)
    localInterface.AddRoute(targetDefault, nextHopVPNGateway, 25)
}

func GetLocalInterface() error {
    ifcs, err := winipcfg.GetAdaptersAddresses(windows.AF_INET, winipcfg.GAAFlagIncludeGateways)
    if err != nil {
        return err
    }

    var primaryInterface *winipcfg.IPAdapterAddresses
    for _, ifc := range ifcs {
        // println(ifc.AdapterName(), ifc.Description(), ifc.FriendlyName(), ifc.Ipv4Metric, ifc.IfType)
        // exclude Virtual Ethernet and Loopback Adapter
        if !strings.Contains(ifc.Description(), "Virtual") {
            // https://git.zx2c4.com/wireguard-windows/tree/tunnel/winipcfg/types.go?h=v0.5.3#n61
            if ifc.IfType == 6 || ifc.IfType == 71 {
                if primaryInterface == nil || (ifc.FirstGatewayAddress != nil && ifc.Ipv4Metric < primaryInterface.Ipv4Metric) {
                    primaryInterface = ifc
                }
            }
        }
    }

    base.LocalInterface.Name = primaryInterface.FriendlyName()
    base.LocalInterface.Ip4 = primaryInterface.FirstUnicastAddress.Address.IP().String()
    base.LocalInterface.Gateway = primaryInterface.FirstGatewayAddress.Address.IP().String()
    base.LocalInterface.Mac = net.HardwareAddr(primaryInterface.PhysicalAddress()).String()

    localInterface = primaryInterface.LUID

    return nil
}

func SetMTU(ifname string, mtu int) error {
    cmdStr := fmt.Sprintf("netsh interface ipv4 set subinterface \"%s\" MTU=%d", ifname, mtu)
    err := execCmd([]string{cmdStr})
    return err
}

func execCmd(cmdStrs []string) error {
    for _, cmdStr := range cmdStrs {
        cmd := exec.Command("cmd", "/C", cmdStr)
        b, err := cmd.CombinedOutput()
        if err != nil {
            return fmt.Errorf("%s %s", string(b), cmd.String())
        }
    }
    return nil
}
