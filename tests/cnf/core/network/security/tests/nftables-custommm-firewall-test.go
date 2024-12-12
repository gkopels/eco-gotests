package tests

import (
	"context"
	"fmt"
	butaneConfig "github.com/coreos/butane/config"
	"github.com/coreos/butane/config/common"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/mco"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/internal/coreparams"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/define"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/ipaddr"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/tsparams"
	ocpoperatorv1 "github.com/openshift/api/operator/v1"
	mcv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"

	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	newtype "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var (
	hubIPv4ExternalAddresses = []string{"172.16.0.10", "172.16.0.11"}
	externalNad              *nad.Builder
	metalLbTestsLabel        = map[string]string{"metallb": "metallbtests"}
	workerNodeList           []*nodes.Builder
	masterNodeList           []*nodes.Builder
	ipv4NodeAddrList         []string
	workerLabelMap           map[string]string
)

const (
	requiredWorkerNodeNumber = 2
	// cnfWorkerNodeList []*nodes.Builder
)

var _ = Describe("nftables", Ordered, Label(tsparams.LabelNftablesTestCases), ContinueOnFailure, func() {

	BeforeAll(func() {

		By("List CNF worker nodes in cluster")
		cnfWorkerNodeList, err := nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

		By("Selecting worker node for Security tests")
		// workerNodeList
		workerLabelMap, workerNodeList = setWorkerNodeListAndLabelForTests(cnfWorkerNodeList)
		ipv4NodeAddrList, err = nodes.ListExternalIPv4Networks(
			APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to collect external nodes ip addresses")

		By("Edit the machineconfiguration cluster to include nftables")
		// updateMachineConfigurationCluster(true)

		By("Creating external FRR pod on master node")
		// _ = deployTestPods(hubIPv4ExternalAddresses[0])

	})

	AfterAll(func() {
		By("Edit the machineconfiguration cluster to remove nftables")
		//updateMachineConfigurationCluster(false)
	})

	Context("custom firewall", func() {
		It("Verify the creation of a new custom node firewall nftables table with an ingress rule",
			reportxml.ID("77412"), func() {
				fmt.Println("Hello World")
				By("Create nftables butane file")
				mcConfig, err := CreateMC("worker")
				Expect(err).ToNot(HaveOccurred(), "Failed to create nftables rules")
				By("Apply the machineConfig")
				applyMachineConfig(mcConfig)
				setupRemoteMultiHopTest(ipv4NodeAddrList, hubIPv4ExternalAddresses, workerNodeList,
					[]string{"hub-pod-worker-0", "hub-pod-worker-1"}, "172.16.0.1")

				time.Sleep(10 * time.Minute)
			})
	})

})

func deployTestPods(hubIPv4ExternalAddresses string) *pod.Builder {

	By("Creating External NAD")
	createExternalNad(tsparams.ExternalMacVlanNADName)

	By("Creating static ip annotation")

	By("Creating FRR Pod")

	return nil
}

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
	By("should have MetalLB controller in running state")
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

func applyMachineConfig(mc []byte) {
	obj := mcv1.MachineConfig{}
	err := yaml.Unmarshal(mc, obj)
	Expect(err).ToNot(HaveOccurred(), "Failed to create external NetworkAttachmentDefinition")
	fmt.Println(string(mc))
	err = APIClient.AttachScheme(mcv1.Install)
	err = APIClient.Create(context.TODO(), &obj)
	Expect(err).ToNot(HaveOccurred(), "Failed to create external NetworkAttachmentDefinition")

	_, err = mco.NewMCBuilder(APIClient, "98-nftables-cnf-worker").
		WithLabel("machineconfiguration.openshift.io/role", NetConfig.CnfMcpLabel).
		WithRawConfig(mc).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create external NetworkAttachmentDefinition")
}

func setupRemoteMultiHopTest(ipv4metalLbIPList, hubIPv4ExternalAddresses []string,
	workerNodeList []*nodes.Builder, hubPodWorkerName []string, frrExternalMasterIPAddress string) (*pod.Builder, []*pod.Builder) {
	By("Setting test parameters")
	frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
		LabelSelector: "app=frr-k8s",
	})
	Expect(err).ToNot(HaveOccurred(), "Failed to list frrk8 pods")

	By("Creating External NAD for master FRR pod")
	createExternalNad(tsparams.ExternalMacVlanNADName)

	By("Creating External NAD for hub FRR pods")
	createExternalNad("nad-hub")

	By("Creating static ip annotation for hub0")

	hub0BRstaticIPAnnotation := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName,
		"nad-hub",
		[]string{fmt.Sprintf("%s/24", ipv4metalLbIPList[0])},
		[]string{fmt.Sprintf("%s/24", hubIPv4ExternalAddresses[0])})

	By("Creating static ip annotation for hub1")

	hub1BRstaticIPAnnotation := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName,
		"nad-hub",
		[]string{fmt.Sprintf("%s/24", ipv4metalLbIPList[1])},
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
	time.Sleep(20 * time.Minute)
	frrPod := createMasterFrrPod(frrExternalMasterIPAddress, ipv4NodeAddrList, hubIPv4ExternalAddresses)

	By("Adding static routes to the speakers")

	speakerRoutesMap := buildRoutesMapWithSpecificRoutes(frrk8sPods, ipv4metalLbIPList)

	for _, frrk8sPod := range frrk8sPods {
		out, err := SetStaticRoute(frrk8sPod, "add", "172.16.0.1", speakerRoutesMap)
		Expect(err).ToNot(HaveOccurred(), out)
	}

	return frrPod, frrk8sPods
}

func createStaticIPAnnotations(internalNADName, externalNADName string, internalIPAddresses,
	externalIPAddresses []string) []*types.NetworkSelectionElement {
	ipAnnotation := pod.StaticIPAnnotation(internalNADName, internalIPAddresses)
	ipAnnotation = append(ipAnnotation,
		pod.StaticIPAnnotation(externalNADName, externalIPAddresses)...)

	return ipAnnotation
}

func createHubConfigMap(name string) *configmap.Builder {
	configMapData := DefineBaseConfig(tsparams.DaemonsFile, "")
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

func setWorkerNodeListAndLabelForTests(
	workerNodeList []*nodes.Builder) (map[string]string, []*nodes.Builder) {

	return NetConfig.WorkerLabelMap, workerNodeList
}

func createMasterFrrPod(frrExternalMasterIPAddress string, ipv4NodeAddrList,
	hubIPAddresses []string) *pod.Builder {
	masterConfigMap := createConfigMapWithStaticRoutes(ipv4NodeAddrList, hubIPAddresses)

	By("Creating static ip annotation for master FRR pod")

	masterStaticIPAnnotation := pod.StaticIPAnnotation(
		"nad-hub", []string{fmt.Sprintf("%s/24", frrExternalMasterIPAddress)})

	By("Creating FRR Master Pod")

	frrPod := createFrrPod(
		masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, masterStaticIPAnnotation)

	return frrPod
}

func createConfigMapWithStaticRoutes(nodeAddrList, hubIPAddresses []string) *configmap.Builder {
	configMapData := DefineBaseConfig(tsparams.DaemonsFile, "")
	masterConfigMap, err := configmap.NewBuilder(APIClient, "frr-master-node-config", tsparams.TestNamespaceName).
		WithData(configMapData).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create config map")

	return masterConfigMap
}

func createFrrPod(
	nodeName string,
	configmapName string,
	defaultCMD []string,
	secondaryNetConfig []*types.NetworkSelectionElement,
	podName ...string) *pod.Builder {
	name := "frr"

	if len(podName) > 0 {
		name = podName[0]
	}

	frrPod := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName, NetConfig.FrrImage).
		DefineOnNode(nodeName).
		WithTolerationToMaster().
		WithSecondaryNetwork(secondaryNetConfig).
		RedefineDefaultCMD(defaultCMD)

	By("Creating FRR container")

	if configmapName != "" {
		frrContainer := pod.NewContainerBuilder(
			"frr2",
			NetConfig.CnfNetTestContainer,
			[]string{"/bin/bash", "-c", "ip route del default && sleep INF"}).
			WithSecurityCapabilities([]string{"NET_ADMIN", "NET_RAW", "SYS_ADMIN"}, true)

		frrCtr, err := frrContainer.GetContainerCfg()
		Expect(err).ToNot(HaveOccurred(), "Failed to get container configuration")
		frrPod.WithAdditionalContainer(frrCtr).WithLocalVolume(configmapName, "/etc/frr")
	}

	By("Creating FRR pod in the test namespace")

	frrPod, err := frrPod.WithPrivilegedFlag().CreateAndWaitUntilRunning(time.Minute)
	Expect(err).ToNot(HaveOccurred(), "Failed to create FRR test pod")

	return frrPod
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
func DefineBaseConfig(daemonsConfig, vtyShConfig string) map[string]string {
	configMapData := make(map[string]string)
	configMapData["daemons"] = daemonsConfig
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

// SetStaticRoute could set or delete static route on all Speaker pods.
func SetStaticRoute(frrPod *pod.Builder, action, destIP string, nextHopMap map[string]string) (string, error) {
	buffer, err := frrPod.ExecCommand(
		[]string{"ip", "route", action, destIP, "via", nextHopMap[frrPod.Definition.Spec.NodeName]}, "frr")
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

func CreateMC(nodeRole string) (machineConfig []byte, err error) {
	butaneConfigVar := createButaneConfig(nodeRole)
	options := common.TranslateBytesOptions{}
	machineConfig, _, err = butaneConfig.TranslateBytes(butaneConfigVar, options)
	if err != nil {
		return nil, fmt.Errorf("failed to covert the ButaneConfig to yaml %v: ", err)
	}
	//nodeName := "worker-0"

	//// Ensure the artifacts directory exists
	//if _, err := os.Stat(artifactsDir); os.IsNotExist(err) {
	//	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
	//		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to create artifacts directory: %s", artifactsDir))
	//	}
	//}
	//// Write the machine config to a file
	//filePath := filepath.Join(artifactsDir, "nftables-after-reboot-"+nodeName)
	//if err := os.WriteFile(filePath, machineConfig, 0644); err != nil {
	//	return nil, fmt.Errorf("failed to write machine config to file: %w", err)
	//}

	fmt.Println("machineConfig", string(machineConfig))

	return machineConfig, nil
}

func createButaneConfig(nodeRole string) []byte {
	butaneConfig := fmt.Sprintf(`variant: openshift
version: %s
metadata:
  labels:
    machineconfiguration.openshift.io/role: worker
  name: "98-nftables-cnf-worker"
systemd:
  units:
    - name: "nftables.service"
      enabled: true
      contents: |
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
        ExecStop=/sbin/nft 'add table inet openshift_filter; delete table inet openshift_filter'
        RemainAfterExit=yes
        [Install]
        WantedBy=multi-user.target
storage:
  files:
    - path: /etc/sysconfig/nftables.conf
      mode: 0600
      overwrite: true
      contents:
        inline: |
          table inet custom_table
          delete table inet custom_table
          table inet custom_table {
          	chain custom_chain_INPUT {
          		type filter hook input priority 1; policy accept;
          		# Drop TCP port 8888 and log
          		tcp dport 8888 log prefix "[USERFIREWALL] PACKET DROP: " drop
          	}
          }`, "4.17.0")
	butaneConfig = strings.ReplaceAll(butaneConfig, "\t", "  ")
	return []byte(butaneConfig)
}
