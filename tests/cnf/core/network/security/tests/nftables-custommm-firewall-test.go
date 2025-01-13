package tests

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/mco"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/internal/coreparams"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/cmd"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/define"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/ipaddr"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/tsparams"
	ocpoperatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"

	ignition "github.com/coreos/ignition/v2/config/v3_4/types"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	newtype "k8s.io/apimachinery/pkg/types"
)

var (
	hubIPv4ExternalAddresses = []string{"172.16.0.10", "172.16.0.11"}
	hubIPv4Network           = "172.16.0.0/24"
	workerNodeList           []*nodes.Builder
	ipv4NodeAddrList         []string
	ipv4SecurityIPList       []string
	workerLabelMap           map[string]string
	testPodWorker0           *pod.Builder
	testPodWorker1           *pod.Builder
	testPodList              []*pod.Builder
	routeMap                 map[string]string
)

var _ = Describe("nftables", Ordered, Label(tsparams.LabelNftablesTestCases), ContinueOnFailure, func() {

	BeforeAll(func() {

		By("List CNF worker nodes in cluster")
		cnfWorkerNodeList, err := nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

		By("Selecting worker node for Security tests")
		workerLabelMap, workerNodeList = setWorkerNodeListAndLabelForTests(cnfWorkerNodeList)
		ipv4NodeAddrList, err = nodes.ListExternalIPv4Networks(
			APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to collect external nodes ip addresses")

		By("Edit the machineconfiguration cluster to include NFTables")
		updateMachineConfigurationCluster(true)

		By("Create test pods on worker nodes")
		testPodWorker0 = createTestPodsOnWorkers(workerNodeList[0].Definition.Name, 8888)
		testPodWorker1 := createTestPodsOnWorkers(workerNodeList[1].Definition.Name, 8888)
		testPodList = []*pod.Builder{testPodWorker0, testPodWorker1}

		By("Create a static route to the external Pod network on each worker node")
		ipv4SecurityIPList, err = NetConfig.GetSecurityIPList()
		Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the ipv4SecurityIPList")

		routeMap = buildRoutesMapWithSpecificRoutes(testPodList, ipv4SecurityIPList)

		for _, testPod := range testPodList {
			outPut, err := setStaticRoute(testPod, "add", hubIPv4Network, routeMap)
			Expect(err).ToNot(HaveOccurred(), outPut, "Failed to create a static route on the worker node")
		}
	})

	AfterAll(func() {
		By("Edit the machineconfiguration cluster to remove NFTables")
		// updateMachineConfigurationCluster(false)

		By("Remove the static route to the external Pod network on each worker node")
		for _, testPod := range testPodList {
			outPut, err := setStaticRoute(testPod, "del", hubIPv4Network, routeMap)
			Expect(err).ToNot(HaveOccurred(), outPut, "Failed to create a static route on the worker node")
		}
	})

	Context("custom firewall", func() {
		AfterEach(func() {
			By("Define and delete a NFTables custom rule")
			createMCAndWaitforMCPStable(tsparams.CustomFireWallDelete)
			err := mco.NewMCBuilder(APIClient, "98-nftables-cnf-worker").Delete()
			Expect(err).ToNot(HaveOccurred(), "Failed to get the machineConfig")

		})

		It("Verify the creation of a new custom node firewall NFTables table with an ingress rule",
			reportxml.ID("77412"), func() {

				By("Setup test environment")
				masterPod := setupRemoteMultiHopTest(ipv4SecurityIPList, hubIPv4ExternalAddresses, ipv4NodeAddrList, workerNodeList,
					[]string{"hub-pod-worker-0", "hub-pod-worker-1"})

				By("Verify ICMP connectivity between the master Pod and the test pods on the workers")
				err := cmd.ICMPConnectivityCheck(masterPod, ipv4NodeAddrList, "net1")
				Expect(err).ToNot(HaveOccurred(), "Failed to ping the worker nodes")

				By("Verify TCP traffic over port 8888 between the master Pod and the test pods on the workers")
				err = validateTCPTraffic(masterPod, ipv4NodeAddrList, 8888)
				Expect(err).ToNot(HaveOccurred(), "Failed to send TCP traffic over port 8888 to the worker nodes")

				By("Define and create a NFTables custom rule")
				createMCAndWaitforMCPStable(tsparams.CustomFirewallInputPort8888)
				//time.Sleep(time.Minute)
				By("Verify TCP traffic over port 8888 between the master Pod and the test pods on the workers")
				err = validateTCPTraffic(masterPod, ipv4NodeAddrList, 8888)
				Expect(err).To(HaveOccurred(), "Successfully sent TCP traffic over port 8888 to the worker nodes")
			})
	})
})

func createExternalNad(name string) {
	var externalNad *nad.Builder
	By("Creating external BR-EX NetworkAttachmentDefinition")
	macVlanPlugin, err := define.MasterNadPlugin(coreparams.OvnExternalBridge, "bridge", nad.IPAMStatic())
	Expect(err).ToNot(HaveOccurred(), "Failed to define master nad plugin")
	externalNad, err = nad.NewBuilder(APIClient, name, tsparams.TestNamespaceName).
		WithMasterPlugin(macVlanPlugin).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create external NetworkAttachmentDefinition")
	Expect(externalNad.Exists()).To(BeTrue(), "Failed to detect external NetworkAttachmentDefinition")
}

func updateMachineConfigurationCluster(nftables bool) {
	By("should update machineconfiguration cluster")
	var jsonBytes []byte

	if nftables {
		jsonBytes = []byte(`
	{"spec":{"nodeDisruptionPolicy":
	  {"files": [{"actions":
	[{"restart": {"serviceName": "nftables.service"},"type": "Restart"}],
	"path": "/etc/sysconfig/nftables.conf"}],
	"units":
	[{"actions":
	[{"reload": {"serviceName":"nftables.service"},"type": "Reload"},
	{"type": "DaemonReload"}],"name": "nftables.service"}]}}}`)
	} else {
		jsonBytes = []byte(`{"spec":{
	"nodeDisruptionPolicy": {"files": [],"units": []}
	}}`)

	}

	err := APIClient.Patch(context.TODO(), &ocpoperatorv1.MachineConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}, client.RawPatch(newtype.MergePatchType, jsonBytes))
	if err != nil {
		fmt.Println(err)
	}
}

func setupRemoteMultiHopTest(ipv4SecurityIPList, hubIPv4ExternalAddresses, ipv4NodeAddrList []string,
	workerNodeList []*nodes.Builder, hubPodWorkerName []string) *pod.Builder {

	By("Creating External NAD for master FRR pod")
	createExternalNad(tsparams.ExternalMacVlanNADName)

	By("Creating External NAD for hub FRR pods")
	createExternalNad("nad-hub")

	By("Creating static ip annotation for hub0")
	//ipv4SecurityIPList, err := NetConfig.GetMetalLbVirIP()
	//Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the ipv4SecurityIPList")

	hub0BRstaticIPAnnotation := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName,
		"nad-hub",
		[]string{fmt.Sprintf("%s/24", ipv4SecurityIPList[0])},
		[]string{fmt.Sprintf("%s/24", hubIPv4ExternalAddresses[0])})

	By("Creating static ip annotation for hub1")

	hub1BRstaticIPAnnotation := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName,
		"nad-hub",
		[]string{fmt.Sprintf("%s/24", ipv4SecurityIPList[1])},
		[]string{fmt.Sprintf("%s/24", hubIPv4ExternalAddresses[1])})

	By("Creating MetalLb Hub pod configMap")

	hubConfigMap := createHubConfigMap("hub-node-config")

	By("Creating FRR Hub pod on worker node 0")

	_ = createFrrHubPod(hubPodWorkerName[0],
		workerNodeList[0].Object.Name, hubConfigMap.Definition.Name, []string{}, hub0BRstaticIPAnnotation)

	By("Creating FRR Hub pod on worker node 1")

	_ = createFrrHubPod(hubPodWorkerName[1],
		workerNodeList[1].Object.Name, hubConfigMap.Definition.Name, []string{}, hub1BRstaticIPAnnotation)

	By("Creating configmap and MetalLb Master pod")

	configMapStaticRoutes := defineConfigMapWithStaticRouteAndNetwork(hubIPv4ExternalAddresses, removePrefixFromIPList(ipv4NodeAddrList))
	masterConfigMap := createMasterConfigMap("master-configmap", configMapStaticRoutes)

	masterStaticIPAnnotation := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName,
		"nad-hub",
		[]string{"172.16.0.1/24"},
		[]string{fmt.Sprintf("")})

	frrPod := createMasterPodTest("master-pod", "master-0", masterConfigMap.Definition.Name,
		[]string{}, masterStaticIPAnnotation)

	return frrPod
}

func createStaticIPAnnotations(internalNADName, externalNADName string, internalIPAddresses,
	externalIPAddresses []string) []*types.NetworkSelectionElement {
	ipAnnotation := pod.StaticIPAnnotation(internalNADName, internalIPAddresses)
	ipAnnotation = append(ipAnnotation,
		pod.StaticIPAnnotation(externalNADName, externalIPAddresses)...)

	return ipAnnotation
}

func createHubConfigMap(name string) *configmap.Builder {
	configMapData := DefineBaseConfig(tsparams.DaemonsFile, "", "")
	hubConfigMap, err := configmap.NewBuilder(APIClient, name, tsparams.TestNamespaceName).WithData(configMapData).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create hub config map")

	return hubConfigMap
}

func createMasterConfigMap(name, configMapStaticRoutes string) *configmap.Builder {
	configMapData := DefineBaseConfig(tsparams.DaemonsFile, configMapStaticRoutes, "")
	hubConfigMap, err := configmap.NewBuilder(APIClient, name, tsparams.TestNamespaceName).WithData(configMapData).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create hub config map")

	return hubConfigMap
}

func createFrrHubPod(name, nodeName, configmapName string, defaultCMD []string,
	secondaryNetConfig []*types.NetworkSelectionElement) *pod.Builder {
	frrPod := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName, NetConfig.FrrImage).
		DefineOnNode(nodeName).
		WithTolerationToMaster().
		WithSecondaryNetwork(secondaryNetConfig).
		RedefineDefaultCMD(defaultCMD)

	By("Creating FRR container")

	frrContainer := pod.NewContainerBuilder(
		"frr2", NetConfig.CnfNetTestContainer, []string{"/bin/bash", "-c", "sleep INF"}).
		WithSecurityCapabilities([]string{"NET_ADMIN", "NET_RAW", "SYS_ADMIN"}, true)

	frrCtr, err := frrContainer.GetContainerCfg()
	Expect(err).ToNot(HaveOccurred(), "Failed to get container configuration")
	frrPod.WithAdditionalContainer(frrCtr).WithLocalVolume(configmapName, "/etc/frr")

	By("Creating FRR pod in the test namespace")

	frrPod, err = frrPod.WithPrivilegedFlag().CreateAndWaitUntilRunning(5 * time.Minute)
	Expect(err).ToNot(HaveOccurred(), "Failed to create FRR test pod")

	return frrPod
}

func createMasterPodTest(name, nodeName, configmapName string, defaultCMD []string, secondaryNetConfig []*types.NetworkSelectionElement) *pod.Builder {
	frrPod := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName, NetConfig.FrrImage).
		DefineOnNode(nodeName).
		WithTolerationToMaster().
		WithSecondaryNetwork(secondaryNetConfig).RedefineDefaultCMD(defaultCMD)

	By("Creating FRR container")

	frrContainer := pod.NewContainerBuilder(
		"frr", NetConfig.CnfNetTestContainer, []string{"/bin/bash", "-c", "sleep INF"}).
		WithSecurityCapabilities([]string{"NET_ADMIN", "NET_RAW", "SYS_ADMIN"}, true)

	frrCtr, err := frrContainer.GetContainerCfg()
	Expect(err).ToNot(HaveOccurred(), "Failed to get container configuration")
	frrPod.WithAdditionalContainer(frrCtr).WithLocalVolume(configmapName, "/etc/frr")

	By("Creating FRR pod in the test namespace")

	frrPod, err = frrPod.WithPrivilegedFlag().CreateAndWaitUntilRunning(5 * time.Minute)
	Expect(err).ToNot(HaveOccurred(), "Failed to create FRR test pod")

	return frrPod
}

func setWorkerNodeListAndLabelForTests(
	workerNodeList []*nodes.Builder) (map[string]string, []*nodes.Builder) {

	return NetConfig.WorkerLabelMap, workerNodeList
}

// DefineFRRConfigWithStaticRoute defines BGP config file with static route and network.
func DefineFRRConfigWithStaticRoute(hubPodIPs, neighborsIPAddresses []string) string {
	frrConfig := tsparams.FRRBaseConfig +
		fmt.Sprintf("ip route %s/32 %s\n", neighborsIPAddresses[1], hubPodIPs[0]) +
		fmt.Sprintf("ip route %s/32 %s\n!\n", neighborsIPAddresses[0], hubPodIPs[1])

	frrConfig += "!\nline vty\n!\nend\n"

	return frrConfig
}

// DefineBaseConfig defines minimal required FRR configuration.
func DefineBaseConfig(daemonsConfig, frrConfig, vtyShConfig string) map[string]string {
	configMapData := make(map[string]string)
	configMapData["daemons"] = daemonsConfig
	configMapData["frr.conf"] = frrConfig
	configMapData["vtysh.conf"] = vtyShConfig

	return configMapData
}

func removePrefixFromIPList(ipAddressList []string) []string {
	var ipAddressListWithoutPrefix []string
	for _, ipaddress := range ipAddressList {
		ipAddressListWithoutPrefix = append(ipAddressListWithoutPrefix, ipaddr.RemovePrefix(ipaddress))
	}

	return ipAddressListWithoutPrefix
}

func buildRoutesMapWithSpecificRoutes(podList []*pod.Builder, nextHopList []string) map[string]string {
	Expect(len(podList)).ToNot(BeZero(), "Pod list is empty")
	Expect(len(nextHopList)).ToNot(BeZero(), "Nexthop IP addresses list is empty")
	Expect(len(nextHopList)).To(BeNumerically(">=", len(podList)),
		fmt.Sprintf("Number of speaker IP addresses[%d] is less than the number of pods[%d]",
			len(nextHopList), len(podList)))

	routesMap := make(map[string]string)

	for _, frrPod := range podList {
		if frrPod.Definition.Spec.NodeName == workerNodeList[0].Definition.Name {
			routesMap[frrPod.Definition.Spec.NodeName] = nextHopList[1]
		} else {
			routesMap[frrPod.Definition.Spec.NodeName] = nextHopList[0]
		}
	}

	return routesMap
}

func setStaticRoute(frrPod *pod.Builder, action, destIP string, nextHopMap map[string]string) (string, error) {
	buffer, err := frrPod.ExecCommand(
		[]string{"ip", "route", action, destIP, "via", nextHopMap[frrPod.Definition.Spec.NodeName]})
	if err != nil {
		if strings.Contains(buffer.String(), "File exists") {
			glog.V(90).Infof("Warning: Route to %s already exist", destIP)

			return buffer.String(), nil
		}

		if strings.Contains(buffer.String(), "No such process") {
			glog.V(90).Infof("Warning: Route to %s already absent", destIP)

			return buffer.String(), nil
		}

		return buffer.String(), err
	}

	return buffer.String(), nil
}

// DefineBGPConfigMapWithStaticRouteAndNetwork defines BGP config file with static route and network.
func defineConfigMapWithStaticRouteAndNetwork(hubPodIPs, nodeIPAddresses []string) string {
	bgpConfig :=
		fmt.Sprintf("ip route %s/32 %s\n", nodeIPAddresses[1], hubPodIPs[0]) +
			fmt.Sprintf("ip route %s/32 %s\n!\n", nodeIPAddresses[0], hubPodIPs[1])

	bgpConfig += "!\nline vty\n!\nend\n"

	return bgpConfig
}

func createTestPodsOnWorkers(nodeName string, portNum int) *pod.Builder {
	testPod, err := pod.NewBuilder(
		APIClient, "nginxtpod-"+nodeName, tsparams.TestNamespaceName, NetConfig.CnfNetTestContainer).
		DefineOnNode(nodeName).WithHostNetwork().WithHostPid(true).
		RedefineDefaultCMD([]string{"/bin/bash", "-c", fmt.Sprintf("testcmd -interface br-ex -protocol tcp -port %d -listen", portNum)}).
		WithPrivilegedFlag().CreateAndWaitUntilRunning(180 * time.Second)
	Expect(err).ToNot(HaveOccurred(), "Failed to create nginx test pod")

	return testPod
}

func validateTCPTraffic(clientPod *pod.Builder, destIPAddrs []string, portNum int) error {
	for _, destIPAddr := range removePrefixFromIPList(destIPAddrs) {
		command := fmt.Sprintf("testcmd -interface net1 -protocol tcp -port %d -server %s", portNum, destIPAddr)
		_, err := clientPod.ExecCommand([]string{"bash", "-c", command}, "frr")
		//Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Fail to run testcmd on %s command output: %s",
		//	clientPod.Definition.Name, outPut.String()))
		return err
	}

	return nil
}

func createMCAndWaitforMCPStable(fileContentString string) {
	truePointer := true
	stringgzip := "gzip"

	mode := 384
	fileContents := fileContentString
	sysDContents := `
            [Unit]  
            Description=Netfilter Tables
            Documentation=man:nft(8)
            Wants=network-pre.target
            Before=network-pre.target
            [Service]
            Type=oneshot
            ProtectSystem=full
            ProtectHome=true
            ExecStart=/sbin/nft -f /etc/sysconfig/nftables.conf
            ExecReload=/sbin/nft -f /etc/sysconfig/nftables.conf
            ExecStop=/sbin/nft 'add table inet custom_table; delete table inet custom_table'
            RemainAfterExit=yes
            [Install]
            WantedBy=multi-user.target`
	ignitionConfig := ignition.Config{
		Ignition: ignition.Ignition{
			Version: "3.4.0",
		},
		Systemd: ignition.Systemd{
			Units: []ignition.Unit{
				{
					Enabled:  &truePointer,
					Name:     "nftables.service",
					Contents: &sysDContents,
				},
			},
		},
		Storage: ignition.Storage{
			Files: []ignition.File{
				{
					Node: ignition.Node{
						Overwrite: &truePointer,
						Path:      "/etc/sysconfig/nftables.conf",
					},
					FileEmbedded1: ignition.FileEmbedded1{
						Contents: ignition.Resource{
							Compression: &stringgzip,
							Source:      &fileContents,
						},
						Mode: &mode,
					},
				},
			},
		},
	}
	finalIgnitionConfig, err := json.Marshal(ignitionConfig)
	Expect(err).ToNot(HaveOccurred(), "Failed to serialize ignition config")
	_, err = mco.NewMCBuilder(APIClient, "98-nftables-cnf-worker").
		WithLabel("machineconfiguration.openshift.io/role", NetConfig.CnfMcpLabel).
		WithRawConfig(finalIgnitionConfig).
		Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create RDMA machine config")

	err = WaitForMcpStable(APIClient, 3*time.Minute, 30*time.Second, "workercnf")
	Expect(err).ToNot(HaveOccurred(), "Failed to wait for MCP to be stable")
}

// WaitForMcpStable waits for the stability of the MCP with the given name.
func WaitForMcpStable(apiClient *clients.Settings, waitingTime, stableDuration time.Duration, mcpName string) error {
	mcp, err := mco.Pull(apiClient, mcpName)

	if err != nil {
		return fmt.Errorf("fail to pull mcp %s from cluster due to: %s", mcpName, err.Error())
	}

	err = mcp.WaitToBeStableFor(stableDuration, waitingTime)

	if err != nil {
		return fmt.Errorf("cluster is not stable: %s", err.Error())
	}

	return nil
}
