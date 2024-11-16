package tests

import (
	"fmt"
	"github.com/openshift-kni/eco-goinfra/pkg/schemes/metallb/mlbtypesv1beta2"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/frr"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/deployment"
	"github.com/openshift-kni/eco-goinfra/pkg/metallb"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/metallbenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var _ = Describe("BGP", Ordered, Label(tsparams.LabelNftablesTestCases), ContinueOnFailure, func() {
	var (
		err           error
		dynamicASiBGP = "internal"
		dynamicASeBGP = "external"
	)

	BeforeAll(func() {

		By("Getting MetalLb load balancer ip addresses")
		ipv4metalLbIPList, ipv6metalLbIPList, err = metallbenv.GetMetalLbIPByIPStack()
		Expect(err).ToNot(HaveOccurred(), tsparams.MlbAddressListError)

		By("List CNF worker nodes in cluster")
		cnfWorkerNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

		By("Selecting worker node for BGP tests")
		workerLabelMap, workerNodeList = setWorkerNodeListAndLabelForBfdTests(cnfWorkerNodeList, metalLbTestsLabel)
		ipv4NodeAddrList, err = nodes.ListExternalIPv4Networks(
			APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to collect external nodes ip addresses")

		err = metallbenv.IsEnvVarMetalLbIPinNodeExtNetRange(ipv4NodeAddrList, ipv4metalLbIPList, nil)
		Expect(err).ToNot(HaveOccurred(), "Failed to validate metalLb exported ip address")

		By("Listing master nodes")
		masterNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.ControlPlaneLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Fail to list master nodes")
		Expect(len(masterNodeList)).To(BeNumerically(">", 0),
			"Failed to detect master nodes")
	})

	AfterAll(func() {
		if len(cnfWorkerNodeList) > 2 {
			By("Remove custom metallb test label from nodes")
			removeNodeLabel(workerNodeList, metalLbTestsLabel)
		}
	})

	Context("remote-dynamicAS", func() {

		var (
			addressPool                  []string
			frrK8WebHookServer           = "frr-k8s-webhook-server"
			hubIPv4ExternalAddresses     = []string{"172.16.0.10", "172.16.0.11"}
			externalAdvertisedIPv4Routes = []string{"192.168.100.0/24", "192.168.200.0/24"}
			externalAdvertisedIPv6Routes = []string{"2001:100::0/64", "2001:200::0/64"}
			err                          error
		)

		BeforeAll(func() {
			By("Setting test iteration parameters")
			_, _, _, _, addressPool, _, err =
				metallbenv.DefineIterationParams(
					ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, netparam.IPV4Family)
			Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

		})

		AfterEach(func() {
			By("Clean metallb operator and test namespaces")
			resetOperatorAndTestNS()
		})

		FIt("Verify the establishment of an eBGP adjacency using neighbor peer remote-as external",
			reportxml.ID("76821"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				By("Waiting until the new frr-k8s-webhook-server deployment is in Ready state.")
				frrk8sWebhookDeployment, err := deployment.Pull(
					APIClient, frrK8WebHookServer, NetConfig.MlbOperatorNamespace)
				Expect(err).ToNot(HaveOccurred(), "Fail to pull frr-k8s-webhook-server")
				Expect(frrk8sWebhookDeployment.IsReady(30*time.Second)).To(BeTrue(),
					"frr-k8s-webhook-server deployment is not ready")

				frrPod := deployFrrExternalPod(addressPool, hubIPv4ExternalAddresses, externalAdvertisedIPv4Routes,
					externalAdvertisedIPv6Routes, 64501)

				By("Creating eBGP Peers with dynamicASN")
				createBGPPeerWithDynamicASN(ipv4metalLbIPList[0], dynamicASeBGP, false)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
			})

		FIt("Verify the establishment of an iBGP adjacency using neighbor peer remote-as internal",
			reportxml.ID("76822"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				By("Waiting until the new frr-k8s-webhook-server deployment is in Ready state.")
				frrk8sWebhookDeployment, err := deployment.Pull(
					APIClient, frrK8WebHookServer, NetConfig.MlbOperatorNamespace)
				Expect(err).ToNot(HaveOccurred(), "Fail to pull frr-k8s-webhook-server")
				Expect(frrk8sWebhookDeployment.IsReady(30*time.Second)).To(BeTrue(),
					"frr-k8s-webhook-server deployment is not ready")

				frrPod := deployFrrExternalPod(addressPool, hubIPv4ExternalAddresses, externalAdvertisedIPv4Routes,
					externalAdvertisedIPv6Routes, 64500)

				By("Creating iBGP Peers with dynamicASN")
				createBGPPeerWithDynamicASN(ipv4metalLbIPList[0], dynamicASiBGP, false)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
			})

		It("Verify the failure to establish a iBGP adjacency using neighbor peer remote-as external",
			reportxml.ID("76825"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				By("Waiting until the new frr-k8s-webhook-server deployment is in Ready state.")
				frrk8sWebhookDeployment, err := deployment.Pull(
					APIClient, frrK8WebHookServer, NetConfig.MlbOperatorNamespace)
				Expect(err).ToNot(HaveOccurred(), "Fail to pull frr-k8s-webhook-server")
				Expect(frrk8sWebhookDeployment.IsReady(30*time.Second)).To(BeTrue(),
					"frr-k8s-webhook-server deployment is not ready")

				frrPod := deployFrrExternalPod(addressPool, hubIPv4ExternalAddresses, externalAdvertisedIPv4Routes,
					externalAdvertisedIPv6Routes, 64500)

				By("Creating eBGP Peers with dynamicASN")
				createBGPPeerWithDynamicASN(ipv4metalLbIPList[0], dynamicASeBGP, false)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
			})
	})
})

func createBGPPeerWithDynamicASN(peerIP, dynamicASN string, eBgpMultiHop bool) {
	By("Creating BGP Peer")

	bgpPeer := metallb.NewBPGPeerBuilder(APIClient, "testpeer", NetConfig.MlbOperatorNamespace,
		peerIP, tsparams.LocalBGPASN, 0).WithDynamicASN(mlbtypesv1beta2.DynamicASNMode(dynamicASN)).
		WithPassword(tsparams.BGPPassword).WithEBGPMultiHop(eBgpMultiHop)

	_, err := bgpPeer.Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create BGP peer")
}

func deployFrrExternalPod(addressPool, hubIPAddresses, externalAdvertisedIPv4Routes,
	externalAdvertisedIPv6Routes []string, localAS int) *pod.Builder {
	By("Creating an IPAddressPool and BGPAdvertisement")

	ipAddressPool := setupBgpAdvertisement(addressPool, int32(32))

	By("Creating a MetalLB service")
	setupMetalLbService(netparam.IPV4Family, ipAddressPool, "Cluster")

	By("Creating nginx test pod on worker node")
	setupNGNXPod(workerNodeList[0].Definition.Name)

	By("Creating External NAD")
	createExternalNad(tsparams.ExternalMacVlanNADName)

	By("Creating static ip annotation")

	staticIPAnnotation := pod.StaticIPAnnotation(
		externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

	By("Creating MetalLb configMap")

	masterConfigMap := createConfigMapWithStaticRoutes(localAS, ipv4NodeAddrList, hubIPAddresses,
		externalAdvertisedIPv4Routes, externalAdvertisedIPv6Routes, false, false)

	By("Creating FRR Pod")

	frrPod := createFrrPod(
		masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

	return frrPod
}

func verifyMetalLbBGPOpenMessage(frrPod *pod.Builder, peerAddrList []string) {
	for _, peerAddress := range removePrefixFromIPList(peerAddrList) {
		Eventually(frr.BGPNeighborshipHasState,
			time.Minute*3, tsparams.DefaultRetryInterval).
			WithArguments(frrPod, peerAddress, "Established").Should(
			BeTrue(), "Failed to receive BGP status UP")
	}
}
