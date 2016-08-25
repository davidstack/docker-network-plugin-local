package main

import (
	"errors"
	"fmt"
	"os"

	"strings"
	"sync"

	"net"

	"encoding/json"
	"io/ioutil"

	"syscall"

	_ "github.com/Unknwon/goconfig"

	"github.com/docker/go-plugins-helpers/network"
	"github.com/milosgajdos83/tenus"
	"github.com/vishvananda/netlink"
)

const (
	PluginDataDir      = "/var/lib/docker-pipe-network/metadata/"
	DriverCacheFile    = "/var/lib/docker-pipe-network/metadata/cache.json"
	PipeWorkConfigFile = "/var/lib/docker-pipe-network/plugin.ini"
)

type EndPointCache struct {
	Mutex     *sync.Mutex
	EndPoints map[string]*EndPoint
	Network   *NetworkInfo //network info of this network driver
}
type PipeNetworkDriver struct {
	Name string
}

var GlobalEndPointCache EndPointCache

//load from cache.json or from create network
type NetworkInfo struct {
	BridgeName         string
	MTU                int
	Gateway            string
	GatewayMask        string
	ContainerInterface string
	NetWorkId          string
}
type EndPoint struct {
	EndpointID   string
	Address      string
	SandboxKey   string
	VethName     string
	VethPeerName string
}

func NewPipeNetworkDriver() *PipeNetworkDriver {
	fmt.Println("Starting... ")
	driver := initialCache()
	os.Mkdir(PluginDataDir, 0700)
	//watcher, _ := NewWatcher()
	//watcher.StartWatch()
	return &driver
}

func (driver *PipeNetworkDriver) GetCapabilities() (*network.CapabilitiesResponse, error) {
	return &network.CapabilitiesResponse{Scope: "local"}, nil
}

func (driver *PipeNetworkDriver) CreateNetwork(createNetworkRequest *network.CreateNetworkRequest) error {
	GlobalEndPointCache.Mutex.Lock()
	defer func() {
		GlobalEndPointCache.Mutex.Unlock()
	}()
	gateway, mask, _ := getGatewayIP(createNetworkRequest)
	networkInfo := NetworkInfo{BridgeName: "br0",
		Gateway:            gateway,
		GatewayMask:        mask,
		ContainerInterface: "eth1",
		MTU:                1500,
		NetWorkId:          createNetworkRequest.NetworkID,
	}
	GlobalEndPointCache.EndPoints = make(map[string]*EndPoint)
	GlobalEndPointCache.Network = &networkInfo
	driver.UpdateCacheFile()
	return nil
}
func (driver *PipeNetworkDriver) DeleteNetwork(deleteNetworkRequest *network.DeleteNetworkRequest) error {
	driver = &PipeNetworkDriver{
		Name: "local",
	}
	GlobalEndPointCache = EndPointCache{
		Mutex: &sync.Mutex{},
	}
	driver.UpdateCacheFile()
	return nil
}
func (driver *PipeNetworkDriver) CreateEndpoint(createEndpointRequest *network.CreateEndpointRequest) (*network.CreateEndpointResponse, error) {
	fmt.Println("create endpoint")
	GlobalEndPointCache.Mutex.Lock()
	defer func() {
		GlobalEndPointCache.Mutex.Unlock()
	}()
	endPointId := createEndpointRequest.EndpointID
	ipaddress := createEndpointRequest.Interface.Address
	vethPairTag := truncateID(endPointId)
	vethPariA := vethPairTag + "-a"
	vethPariB := vethPairTag + "-b"
	endPoint := EndPoint{EndpointID: endPointId,
		Address:      ipaddress,
		VethName:     vethPariA,
		VethPeerName: vethPariB}
	GlobalEndPointCache.EndPoints[endPointId] = &endPoint
	driver.UpdateCacheFile()
	return nil, nil
}
func (driver *PipeNetworkDriver) DeleteEndpoint(deleteEndpointRequest *network.DeleteEndpointRequest) error {
	fmt.Println("DeleteEndpoint...")
	GlobalEndPointCache.Mutex.Lock()
	defer func() {
		GlobalEndPointCache.Mutex.Unlock()
	}()
	delete(GlobalEndPointCache.EndPoints, deleteEndpointRequest.EndpointID)
	driver.UpdateCacheFile()
	return nil
}
func (driver *PipeNetworkDriver) EndpointInfo(infoRequest *network.InfoRequest) (*network.InfoResponse, error) {
	fmt.Println("endpoint info...")

	return nil, nil
}
func (driver *PipeNetworkDriver) Join(joinRequest *network.JoinRequest) (*network.JoinResponse, error) {
	fmt.Println("joing....")
	fmt.Println("NetworkID is ", joinRequest.NetworkID)
	fmt.Println("SandboxKey is ", joinRequest.SandboxKey)

	GlobalEndPointCache.Mutex.Lock()
	defer func() {
		GlobalEndPointCache.Mutex.Unlock()
	}()
	//query from cache
	endPointInfo := GlobalEndPointCache.EndPoints[joinRequest.EndpointID]
	fmt.Println("vethPairA is ", endPointInfo.VethName)
	fmt.Println("vethPairB is ", endPointInfo.VethPeerName)

	localVethPair := vethPair(endPointInfo.VethName, endPointInfo.VethPeerName)
	if err := netlink.LinkAdd(localVethPair); err != nil {
		fmt.Println("failed to create the veth pair named: [ %v ] error: [ %s ] ", localVethPair, err)
		return nil, err
	}
	fmt.Println("localVethPair.Name is ", localVethPair.Name)
	// 2. add vethPariA to bridge and set up
	createdLink, linkErr := netlink.LinkByName(localVethPair.Name)
	if linkErr != nil {
		fmt.Println("find link failed", linkErr)
		return nil, linkErr
	}
	netinterface := net.Interface{Index: createdLink.Attrs().Index,
		Name:         createdLink.Attrs().Name,
		MTU:          createdLink.Attrs().MTU,
		Flags:        createdLink.Attrs().Flags,
		HardwareAddr: createdLink.Attrs().HardwareAddr}

	br, err := tenus.BridgeFromName(GlobalEndPointCache.Network.BridgeName)
	if err != nil {
		fmt.Println("use exist bridge failed", err)
		return nil, err
	}

	if err = br.AddSlaveIfc(&netinterface); err != nil {
		fmt.Println("add interface to bridge failed", err)
	}
	err = netlink.LinkSetUp(createdLink)
	if err != nil {
		fmt.Println("Error enabling  Veth local iface: [ %v ]", localVethPair)
		return nil, err
	}
	response := network.JoinResponse{InterfaceName: network.InterfaceName{SrcName: endPointInfo.VethPeerName,
		DstPrefix: "eth"},
		Gateway: GlobalEndPointCache.Network.Gateway,
	}
	GlobalEndPointCache.EndPoints[joinRequest.EndpointID].SandboxKey = joinRequest.SandboxKey
	driver.UpdateCacheFile()
	return &response, nil
}
func (driver *PipeNetworkDriver) Leave(leaveRequest *network.LeaveRequest) error {
	fmt.Println("Leave...")

	//remove veth pair
	GlobalEndPointCache.Mutex.Lock()
	defer func() {
		GlobalEndPointCache.Mutex.Unlock()
	}()
	endPointInfo := GlobalEndPointCache.EndPoints[leaveRequest.EndpointID]
	createdLink, linkErr := netlink.LinkByName(endPointInfo.VethName)
	if linkErr != nil {
		fmt.Println("find link failed", linkErr)
		return linkErr
	}
	/*br, err := tenus.BridgeFromName(GlobalEndPointCache.Network.BridgeName)
	if err != nil {
		fmt.Println("use exist bridge failed", err)
		return err
	}

		netinterface := net.Interface{Index: createdLink.Attrs().Index,
			Name:         createdLink.Attrs().Name,
			MTU:          createdLink.Attrs().MTU,
			Flags:        createdLink.Attrs().Flags,
			HardwareAddr: createdLink.Attrs().HardwareAddr}
		if err = br.RemoveSlaveIfc(&netinterface); err != nil {
			fmt.Println("remove interface to bridge failed", err)
			return err
		}*/
	netlink.LinkSetDown(createdLink)
	netlink.LinkDel(createdLink)

	return nil
}
func (driver *PipeNetworkDriver) DiscoverNew(discoveryNotification *network.DiscoveryNotification) error {
	fmt.Println("DiscoverNew...")
	return nil
}
func (driver *PipeNetworkDriver) DiscoverDelete(discoveryNotification *network.DiscoveryNotification) error {
	fmt.Println("DiscoverDelete...")
	return nil
}
func (driver *PipeNetworkDriver) ProgramExternalConnectivity(programExternalConnectivityRequest *network.ProgramExternalConnectivityRequest) error {
	fmt.Println("ProgramExternalConnectivity...")

	return nil
}
func (driver *PipeNetworkDriver) RevokeExternalConnectivity(revokeExternalConnectivityRequest *network.RevokeExternalConnectivityRequest) error {
	fmt.Println("RevokeExternalConnectivity...")
	return nil
}
func initialCache() PipeNetworkDriver {
	driver := PipeNetworkDriver{
		Name: "local",
	}
	GlobalEndPointCache = EndPointCache{
		Mutex: &sync.Mutex{},
	}
	/*
		_, err1 := goconfig.LoadConfigFile(PipeWorkConfigFile)
		if err1 != nil {
			fmt.Println("load config file failed...Terminated!!!", err1)
			panic("config file error")
		}*/
	if _, err := os.Stat(DriverCacheFile); err == nil {
		data := EndPointCache{}
		bytes, _ := ioutil.ReadFile(DriverCacheFile)
		json.Unmarshal(bytes, &data)
		GlobalEndPointCache.Network = data.Network
		GlobalEndPointCache.EndPoints = data.EndPoints
	}
	//driver.Etcd, _ = cfg.GetValue(goconfig.DEFAULT_SECTION, "etcd")
	return driver
}

//create new veth pair
func vethPair(name string, peerName string) *netlink.Veth {
	return &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: name, MTU: GlobalEndPointCache.Network.MTU, Flags: net.FlagPointToPoint},
		PeerName:  peerName,
	}
}
func truncateID(id string) string {
	return id[:5]
}

//parase gateway and mask
func getGatewayIP(r *network.CreateNetworkRequest) (string, string, error) {
	// also in that case, we'll need a function to determine the correct default gateway based on it's IP/Mask
	var gatewayIP string
	if len(r.IPv4Data) > 0 {
		if r.IPv4Data[0] != nil {
			if r.IPv4Data[0].Gateway != "" {
				gatewayIP = r.IPv4Data[0].Gateway
			}
		}
	}

	if gatewayIP == "" {
		return "", "", fmt.Errorf("No gateway IP found")
	}
	parts := strings.Split(gatewayIP, "/")
	if parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("Cannot split gateway IP address")
	}
	return parts[0], parts[1], nil
}

func (driver *PipeNetworkDriver) UpdateCacheFile() {
	fmt.Println("UpdateCacheFile")
	data, err := json.Marshal(GlobalEndPointCache)
	if err != nil {
		fmt.Println(err)
	}
	//fmt.Println("cache data is %s", string(data))
	err = ioutil.WriteFile(DriverCacheFile, data, 0644)
	if err != nil {
		fmt.Println("update cache filed failed", err)
	}
}

//get
func getNsPid(nspath string) (int, error) {
	//nspath = "/var/run/docker/netns/46febe2c1f13"
	fd, err := syscall.Open(nspath, syscall.O_RDONLY, 0)
	if err != nil {
		fmt.Println("get ns pid failed", err)
		return 0, err
	}

	return fd, nil
}

/*
 configure container address when container start
*/
func configContainerIp(endpoint *EndPoint, network *NetworkInfo) error {
	fmt.Println("config container ip")
	//3. get link
	//	sandboxKey := endpoint.SandboxKey
	address := endpoint.Address
	/*fid, fidError := getNsPid(sandboxKey)
	if fidError != nil {
		fmt.Println("get fid failed", fidError)
		return fidError
	}*/
	createdLink, linkErr := netlink.LinkByName(endpoint.VethName)
	if linkErr != nil {
		fmt.Println("get netlink failed", linkErr)
		return linkErr
	}

	//netlink.LinkSetNsFd(createdLink, fid)*/
	//4 set interface name in container
	netlink.LinkSetName(createdLink, "eth0")

	//5 set link ip
	cidr := address
	_, vethHostIpNet, cidrErr := net.ParseCIDR(cidr)
	if cidrErr != nil {
		fmt.Println("parase cid failed", cidrErr)
	}

	addr := netlink.Addr{IPNet: vethHostIpNet}
	netlink.AddrAdd(createdLink, &addr)

	//6 set container default route
	/*dst := &net.IPNet{
		IP:   net.IPv4zero,
		Mask: net.IPv4Mask(0xff, 0, 0, 0),
	}

	gatewayIp := net.ParseIP(network.Gateway)
	delRoute := netlink.Route{LinkIndex: createdLink.Attrs().Index, Dst: dst}
	addRoute := netlink.Route{LinkIndex: createdLink.Attrs().Index, Dst: dst, Gw: gatewayIp}
	routerDelError := netlink.RouteDel(&delRoute)
	if routerDelError != nil {
		fmt.Println("delete default route failed")
	}
	routerAddError := netlink.RouteAdd(&addRoute)
	if routerAddError != nil {
		fmt.Println("add default route failed")
	}*/
	return nil
}
func findNetworkInfo(networkId string, endPointId string) (*EndPoint, *NetworkInfo, error) {
	GlobalEndPointCache.Mutex.Lock()
	defer func() {
		GlobalEndPointCache.Mutex.Unlock()
	}()

	if networkId != GlobalEndPointCache.Network.NetWorkId {
		fmt.Println("this network is not local network:" + networkId)
		fmt.Println("this network is not local network:" + GlobalEndPointCache.Network.NetWorkId)
		return nil, nil, errors.New("network not local")
	}
	if nil != GlobalEndPointCache.EndPoints[endPointId] {
		return GlobalEndPointCache.EndPoints[endPointId], GlobalEndPointCache.Network, nil
	} else {
		return nil, nil, errors.New("endpoint not exist")
	}
}
