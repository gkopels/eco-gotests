package tests

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/metallb"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/namespace"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/metallb/mlbtypes"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/service"
	netcmd "github.com/rh-ecosystem-edge/eco-gotests/tests/cnf/core/network/internal/cmd"
	. "github.com/rh-ecosystem-edge/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/cnf/core/network/internal/netparam"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/cnf/core/network/metallb/internal/frr"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/cnf/core/network/metallb/internal/metallbenv"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/cnf/core/network/metallb/internal/tsparams"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	labelEnvironment = "environment"
	envProduction    = "production"
	envStaged        = "staged"

	svcNameProduction      = "service-sel-production"
	svcNameStaged          = "service-sel-staged"
	svcNameProductionPool2 = "service-sel-production-pool2"

	bgpAdvNameProduction    = "bgp-adv-production"
	bgpAdvNameStagedLocPref = "bgp-adv-staged-localpref500"

	locPrefProduction = uint32(400)
	locPrefStaged     = uint32(500)

	ipaddressPoolName1 = "ipaddresspool1"
	ipaddressPoolName2 = "ipaddresspool2"
)

var _ = Describe("BGPAdvertisement serviceSelector", Ordered, Label(tsparams.LabelBGPTestCases), ContinueOnFailure, func() {
	var (
		frrk8sPods []*pod.Builder
		extFrrPod  *pod.Builder
		ipPool1    *metallb.IPAddressPoolBuilder
		ipPool2    *metallb.IPAddressPoolBuilder
		err        error
	)

	BeforeAll(func() {
		validateEnvVarAndGetNodeList()

		By("Checking cluster supports dual-stack")

		if !clusterSupportsIPv4() || !clusterSupportsIPv6() {
			Skip("BGP serviceSelector tests require a dual-stack cluster (IPv4 + IPv6)")
		}

		By("Collecting frr-k8s pods")

		frrk8sPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
			LabelSelector: tsparams.LabelFRRNode,
		})
		Expect(err).ToNot(HaveOccurred(), "Failed to list frrk8s pods")

		By("Creating a new instance of MetalLB Speakers on workers")

		err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
		Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

		By("Creating dual-stack IPAddressPool 1")

		ipPool1 = createIPAddressPool(ipaddressPoolName1, tsparams.LBipRange1[netparam.DualIPFamily])
		validateAddressPool(ipPool1.Definition.Name, mlbtypes.IPAddressPoolStatus{
			AvailableIPv4: 240,
			AvailableIPv6: 9223372036854775807,
			AssignedIPv4:  0,
			AssignedIPv6:  0,
		})

		By("Creating dual-stack IPAddressPool 2")

		ipPool2 = createIPAddressPool(ipaddressPoolName2, tsparams.LBipRange2[netparam.DualIPFamily])
		validateAddressPool(ipPool2.Definition.Name, mlbtypes.IPAddressPoolStatus{
			AvailableIPv4: 239,
			AvailableIPv6: 9223372036854775807,
			AssignedIPv4:  0,
			AssignedIPv6:  0,
		})

		By("Creating nginx test pod on worker node 0")

		setupNGNXPod(tsparams.MLBNginxPodName+workerNodeList[0].Definition.Name,
			workerNodeList[0].Definition.Name,
			tsparams.LabelValue1)

		By("Creating External NAD for master FRR pod")

		createExternalNad(tsparams.ExternalMacVlanNADName)

		By("Creating static IP annotation for external FRR pod (IPv4 and IPv6)")

		staticIPAnnotation := pod.StaticIPAnnotation(
			tsparams.ExternalMacVlanNADName, []string{
				fmt.Sprintf("%s/%s", metallbAddrList[ipv4][0], netparam.IPSubnet24),
				fmt.Sprintf("%s/%s", metallbAddrList[ipv6][0], netparam.IPSubnet64),
			})

		By("Creating MetalLB configMap for external FRR pod (all worker IPv4 and IPv6 addresses)")

		masterConfigMap := createConfigMap(tsparams.LocalBGPASN,
			append(ipv4NodeAddrList, ipv6NodeAddrList...), false, false)

		By("Creating external FRR Pod on master 0 with dual-stack addresses")

		extFrrPod = createFrrPod(
			masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation, "frr-master0")

		By("Creating BGPPeer for IPv4")

		createBGPPeerAndVerifyIfItsReady(tsparams.BgpPeerName1, metallbAddrList[ipv4][0], "", tsparams.LocalBGPASN,
			false, 0, frrk8sPods)

		By("Creating BGPPeer for IPv6")

		createBGPPeerAndVerifyIfItsReady(tsparams.BgpPeerName2, metallbAddrList[ipv6][0], "", tsparams.LocalBGPASN,
			false, 0, frrk8sPods)

		By("Validating BGP session states on external FRR pod")

		workerAddrs := append(netcmd.RemovePrefixFromIPList(ipv4NodeAddrList),
			netcmd.RemovePrefixFromIPList(ipv6NodeAddrList)...)
		verifyMetalLbBGPSessionsAreUPOnFrrPod(extFrrPod, workerAddrs)
		validateBGPSessionState("Established", "N/A", metallbAddrList[ipv4][0], workerNodeList)
		validateBGPSessionState("Established", "N/A", metallbAddrList[ipv6][0], workerNodeList)
	})

	AfterEach(func() {
		By("Cleaning BGPAdvertisements between tests")

		metalLbNs, err := namespace.Pull(APIClient, NetConfig.MlbOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed to pull metalLb operator namespace")

		err = metalLbNs.CleanObjects(tsparams.DefaultTimeout, metallb.GetBGPAdvertisementGVR())
		Expect(err).ToNot(HaveOccurred(), "Failed to clean BGPAdvertisements")
	})

	AfterAll(func() {
		By("Full cleanup after all serviceSelector tests")

		resetOperatorAndTestNS()

		if len(cnfWorkerNodeList) > 2 {
			By("Remove custom metallb test label from nodes")
			removeNodeLabel(workerNodeList, metalLbTestsLabel)
		}
	})

	Context("Single IPAddressPool", func() {
		var (
			prodIPv4, prodIPv6   string
			stagedIPv4, stagedIPv6 string
		)

		BeforeAll(func() {
			By("Creating production and staged services on pool 1")

			setupDualStackMetalLbServiceWithEnvLabel(svcNameProduction, ipPool1, envProduction)
			setupDualStackMetalLbServiceWithEnvLabel(svcNameStaged, ipPool1, envStaged)

			By("Collecting production and staged service VIPs (IPv4 and IPv6)")

			prodIPv4, prodIPv6 = waitForServiceDualStackVIPs(svcNameProduction)
			stagedIPv4, stagedIPv6 = waitForServiceDualStackVIPs(svcNameStaged)
		})

		AfterAll(func() {
			By("Cleaning Services after context")

			err = namespace.NewBuilder(APIClient, tsparams.TestNamespaceName).CleanObjects(
				tsparams.DefaultTimeout, service.GetGVR())
			Expect(err).ToNot(HaveOccurred(), "Failed to clean Services")
		})

		It("Verify route propagation using the BGP advertisement and service selector option",
			reportxml.ID("87578"), func() {
				By("Creating a BGPAdvertisement with serviceSelector for environment: production")

				createBGPAdvertisementWithServiceSelectors(
					bgpAdvNameProduction,
					nil,
					[]string{tsparams.BgpPeerName1, tsparams.BgpPeerName2},
					[]metav1.LabelSelector{{MatchLabels: map[string]string{labelEnvironment: envProduction}}},
					0,
				)

				By("Verifying production IPv4 /32 and IPv6 /128 are present on external FRR pod")

				Eventually(routePresentOnFRR(extFrrPod, ipv4, ipv4HostRoutePrefix(prodIPv4)), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeTrue(), "production IPv4 VIP must appear in BGP RIB")
				Eventually(routePresentOnFRR(extFrrPod, ipv6, ipv6HostRoutePrefix(prodIPv6)), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeTrue(), "production IPv6 VIP must appear in BGP RIB")

				By("Verifying staged IPv4 /32 and IPv6 /128 are absent on external FRR pod")

				Eventually(routePresentOnFRR(extFrrPod, ipv4, ipv4HostRoutePrefix(stagedIPv4)), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeFalse(), "staged IPv4 VIP must not appear in BGP RIB")
				Eventually(routePresentOnFRR(extFrrPod, ipv6, ipv6HostRoutePrefix(stagedIPv6)), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeFalse(), "staged IPv6 VIP must not appear in BGP RIB")
			})
	})

	Context("Dual IPAddressPools", func() {
		var (
			prodPrefixV4, prodPrefixV6   string
			stagedPrefixV4, stagedPrefixV6 string
		)

		BeforeAll(func() {
			By("Creating two services, one on pool 1 and the other on pool 2")

			setupDualStackMetalLbServiceWithEnvLabel(svcNameProduction, ipPool1, envProduction)
			setupDualStackMetalLbServiceWithEnvLabel(svcNameStaged, ipPool2, envStaged)

			By("Collecting service VIPs (IPv4 and IPv6)")

			prodIPv4, prodIPv6 := waitForServiceDualStackVIPs(svcNameProduction)
			stagedIPv4, stagedIPv6 := waitForServiceDualStackVIPs(svcNameStaged)

			prodPrefixV4 = ipv4HostRoutePrefix(prodIPv4)
			prodPrefixV6 = ipv6HostRoutePrefix(prodIPv6)
			stagedPrefixV4 = ipv4HostRoutePrefix(stagedIPv4)
			stagedPrefixV6 = ipv6HostRoutePrefix(stagedIPv6)
		})

		AfterAll(func() {
			By("Cleaning Services after context")

			err = namespace.NewBuilder(APIClient, tsparams.TestNamespaceName).CleanObjects(
				tsparams.DefaultTimeout, service.GetGVR())
			Expect(err).ToNot(HaveOccurred(), "Failed to clean Services")
		})

		It("Verify route propagation of two non-overlapping BGP advertisements and service selector option",
			reportxml.ID("87692"), func() {
				By(fmt.Sprintf("Creating BGPAdvertisement for production pool with localPref %d", locPrefProduction))

				createBGPAdvertisementWithServiceSelectors(
					bgpAdvNameProduction,
					[]string{ipaddressPoolName1},
					[]string{tsparams.BgpPeerName1, tsparams.BgpPeerName2},
					[]metav1.LabelSelector{{MatchLabels: map[string]string{labelEnvironment: envProduction}}},
					locPrefProduction,
				)

				By(fmt.Sprintf("Creating BGPAdvertisement for staged pool with localPref %d", locPrefStaged))

				createBGPAdvertisementWithServiceSelectors(
					bgpAdvNameStagedLocPref,
					[]string{ipaddressPoolName2},
					[]string{tsparams.BgpPeerName1, tsparams.BgpPeerName2},
					[]metav1.LabelSelector{{MatchLabels: map[string]string{labelEnvironment: envStaged}}},
					locPrefStaged,
				)

			By("Verifying both production and staged prefixes appear in BGP RIB")

				Eventually(routePresentOnFRR(extFrrPod, ipv4, prodPrefixV4), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeTrue())
				Eventually(routePresentOnFRR(extFrrPod, ipv4, stagedPrefixV4), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeTrue())
				Eventually(routePresentOnFRR(extFrrPod, ipv6, prodPrefixV6), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeTrue())
				Eventually(routePresentOnFRR(extFrrPod, ipv6, stagedPrefixV6), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeTrue())

				By(fmt.Sprintf("Validating LocalPref: production IPv4=%d, staged IPv4=%d",
					locPrefProduction, locPrefStaged))

				Eventually(localPrefForPrefix(extFrrPod, ipv4, prodPrefixV4), time.Minute,
					tsparams.DefaultRetryInterval).Should(Equal(locPrefProduction))
				Eventually(localPrefForPrefix(extFrrPod, ipv4, stagedPrefixV4), time.Minute,
					tsparams.DefaultRetryInterval).Should(Equal(locPrefStaged))

				By(fmt.Sprintf("Validating LocalPref: production IPv6=%d, staged IPv6=%d",
					locPrefProduction, locPrefStaged))

				Eventually(localPrefForPrefix(extFrrPod, ipv6, prodPrefixV6), time.Minute,
					tsparams.DefaultRetryInterval).Should(Equal(locPrefProduction))
				Eventually(localPrefForPrefix(extFrrPod, ipv6, stagedPrefixV6), time.Minute,
					tsparams.DefaultRetryInterval).Should(Equal(locPrefStaged))
			})
	})

	Context("Dual IPAddressPools same environment label on different pools", func() {
		var (
			prod1Prefixes         []struct{ family, key string }
			stagedIPv4, stagedIPv6 string
		)

		BeforeAll(func() {
			By("Creating two production services (pool 1 and pool 2) and one staged service on pool 2")

			setupDualStackMetalLbServiceWithEnvLabel(svcNameProduction, ipPool1, envProduction)
			setupDualStackMetalLbServiceWithEnvLabel(svcNameProductionPool2, ipPool2, envProduction)
			setupDualStackMetalLbServiceWithEnvLabel(svcNameStaged, ipPool2, envStaged)

			By("Collecting VIPs for all three services (IPv4 and IPv6)")

			prod1IPv4, prod1IPv6 := waitForServiceDualStackVIPs(svcNameProduction)
			prod2IPv4, prod2IPv6 := waitForServiceDualStackVIPs(svcNameProductionPool2)
			stagedIPv4, stagedIPv6 = waitForServiceDualStackVIPs(svcNameStaged)

			prod1Prefixes = []struct{ family, key string }{
				{ipv4, ipv4HostRoutePrefix(prod1IPv4)},
				{ipv4, ipv4HostRoutePrefix(prod2IPv4)},
				{ipv6, ipv6HostRoutePrefix(prod1IPv6)},
				{ipv6, ipv6HostRoutePrefix(prod2IPv6)},
			}
		})

		AfterAll(func() {
			By("Cleaning Services after context")

			err = namespace.NewBuilder(APIClient, tsparams.TestNamespaceName).CleanObjects(
				tsparams.DefaultTimeout, service.GetGVR())
			Expect(err).ToNot(HaveOccurred(), "Failed to clean Services")
		})

		It("Verify a BGP Advertisement with serviceSelector functions with two services using the same label but "+
			"different ipaddresspools",
			reportxml.ID("87695"), func() {
				By("Creating a BGPAdvertisement with serviceSelector for environment: production " +
					"(no ipAddressPools restriction — matches both pools)")

				createBGPAdvertisementWithServiceSelectors(
					bgpAdvNameProduction,
					nil,
					[]string{tsparams.BgpPeerName1, tsparams.BgpPeerName2},
					[]metav1.LabelSelector{{MatchLabels: map[string]string{labelEnvironment: envProduction}}},
					0,
				)

				By("Verifying both production VIPs (pool1 and pool2) are present in BGP RIB")

				for _, prefix := range prod1Prefixes {
					Eventually(routePresentOnFRR(extFrrPod, prefix.family, prefix.key), time.Minute,
						tsparams.DefaultRetryInterval).Should(BeTrue(),
						fmt.Sprintf("production VIP %s must appear in BGP RIB", prefix.key))
				}

				By("Verifying staged VIPs are absent from BGP RIB")

				Eventually(routePresentOnFRR(extFrrPod, ipv4, ipv4HostRoutePrefix(stagedIPv4)), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeFalse(), "staged IPv4 VIP must not appear in BGP RIB")
				Eventually(routePresentOnFRR(extFrrPod, ipv6, ipv6HostRoutePrefix(stagedIPv6)), time.Minute,
					tsparams.DefaultRetryInterval).Should(BeFalse(), "staged IPv6 VIP must not appear in BGP RIB")
			})
	})

	It("Verify BGP Advertisements are merged using two overlapping BGP advertisements and service selector option",
		reportxml.ID("87691"), func() {
			Skip("Polarion execution steps not yet captured for 87691")
		})

	It("Verify a BGP Advertisement with serviceSelector can be updated",
		reportxml.ID("87693"), func() {
			Skip("Polarion execution steps not yet captured for 87693")
		})

	It("Verify Webhook error when BGP Advertisement and aggregation length are configured together",
		reportxml.ID("87694"), func() {
			Skip("Polarion execution steps not yet captured for 87694")
		})

	It("Verify a BGP Advertisement with serviceSelector functions with two services using the same label and same "+
		"ipaddresspool",
		reportxml.ID("87696"), func() {
			Skip("Polarion execution steps not yet captured for 87696")
		})

	It("Verify BGP Advertisement removes the advertisement of routes after the update of a service label",
		reportxml.ID("87697"), func() {
			Skip("Polarion execution steps not yet captured for 87697")
		})

	It("Verify that two BGP advertisements with different local Pref settings one with serviceSelector and the "+
		"second without only the default BGP Advertisement will be advertised",
		reportxml.ID("87698"), func() {
			Skip("Polarion execution steps not yet captured for 87698")
		})

	It("Verify BGPAdvertisement and L2Advertisement serviceSelector functions correctly and run in parallel",
		reportxml.ID("87699"), func() {
			Skip("Polarion execution steps not yet captured for 87699")
		})
})

func setupDualStackMetalLbServiceWithEnvLabel(
	name string,
	ipAddressPool *metallb.IPAddressPoolBuilder,
	envValue string,
) {
	servicePort, err := service.DefineServicePort(80, 80, "TCP")
	Expect(err).ToNot(HaveOccurred(), "Failed to define service port")

	_, err = service.NewBuilder(APIClient, name, tsparams.TestNamespaceName,
		map[string]string{"app": tsparams.LabelValue1, labelEnvironment: envValue}, *servicePort).
		WithExternalTrafficPolicy(corev1.ServiceExternalTrafficPolicyTypeCluster).
		WithIPFamily(
			[]corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			corev1.IPFamilyPolicyRequireDualStack).
		WithAnnotation(map[string]string{"metallb.io/address-pool": ipAddressPool.Definition.Name}).
		Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create dual-stack MetalLB Service")
}

func createBGPAdvertisementWithServiceSelectors(
	name string,
	ipAddressPools []string,
	peers []string,
	serviceSelectors []metav1.LabelSelector,
	localPref uint32,
) {
	builder := metallb.NewBGPAdvertisementBuilder(APIClient, name, NetConfig.MlbOperatorNamespace)

	if len(ipAddressPools) > 0 {
		builder = builder.WithIPAddressPools(ipAddressPools)
	}

	builder = builder.WithPeers(peers).WithCommunities([]string{tsparams.NoAdvertiseCommunity})

	if localPref > 0 {
		builder = builder.WithLocalPref(localPref)
	}

	agg4 := int32(netparam.IPSubnetInt32)
	agg6 := int32(netparam.IPSubnetInt128)
	builder.Definition.Spec.AggregationLength = &agg4
	builder.Definition.Spec.AggregationLengthV6 = &agg6
	builder.Definition.Spec.ServiceSelectors = serviceSelectors

	_, err := builder.Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create BGPAdvertisement with serviceSelectors")
}

// waitForServiceDualStackVIPs waits until the service has both an IPv4 and an IPv6 LoadBalancer ingress IP
// and returns them.
func waitForServiceDualStackVIPs(name string) (ipv4VIP, ipv6VIP string) {
	Eventually(func() bool {
		svc, e := service.Pull(APIClient, name, tsparams.TestNamespaceName)
		if e != nil {
			return false
		}

		for _, ingress := range svc.Object.Status.LoadBalancer.Ingress {
			if strings.Contains(ingress.IP, ":") {
				ipv6VIP = ingress.IP
			} else {
				ipv4VIP = ingress.IP
			}
		}

		return ipv4VIP != "" && ipv6VIP != ""
	}, time.Minute, tsparams.DefaultRetryInterval).Should(BeTrue(),
		"LoadBalancer IPv4 and IPv6 IPs not assigned for service "+name)

	return ipv4VIP, ipv6VIP
}

func ipv4HostRoutePrefix(ip string) string {
	return fmt.Sprintf("%s/32", strings.TrimSpace(ip))
}

func ipv6HostRoutePrefix(ip string) string {
	return fmt.Sprintf("%s/128", strings.TrimSpace(ip))
}

// routePresentOnFRR returns a polling func that checks whether routeKey (e.g. "1.2.3.4/32" or "2001:db8::1/128")
// is present in the BGP RIB on the given external FRR pod. "test" is the default container name set by
// pod.NewBuilder.
func routePresentOnFRR(frrPod *pod.Builder, ipFamily, routeKey string) func() bool {
	return func() bool {
		bgpStatus, err := frr.GetBGPStatus(frrPod, strings.ToLower(ipFamily), "test")
		if err != nil {
			return false
		}

		_, ok := bgpStatus.Routes[routeKey]

		return ok
	}
}

// localPrefForPrefix returns a polling func that reads LocalPref for the best-path of routeKey
// from the BGP RIB on the given external FRR pod.
func localPrefForPrefix(frrPod *pod.Builder, ipFamily, routeKey string) func() uint32 {
	return func() uint32 {
		bgpStatus, err := frr.GetBGPStatus(frrPod, strings.ToLower(ipFamily), "test")
		if err != nil {
			return 0
		}

		routes, ok := bgpStatus.Routes[routeKey]
		if !ok || len(routes) == 0 {
			return 0
		}

		return routes[0].LocalPref
	}
}
