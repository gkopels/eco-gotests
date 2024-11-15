package securityenv

import (
	"encoding/json"
	"fmt"
	"github.com/openshift-kni/eco-gotests/vendor/github.com/coreos/butane/config/common"

	butaneConfig "github.com/coreos/butane/config"
	ignition "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/golang/glog"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/deployment"
	"github.com/openshift-kni/eco-goinfra/pkg/mco"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/tsparams"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"time"
)

const butaneVersion = "4.17.0"

// DoesClusterSupportNftablesTests verifies if given environment supports metalLb tests.
func DoesClusterSupportNftablesTests(requiredWorkerNodeNumber int) error {
	glog.V(90).Infof("Verifying if MetalLb operator deployed")

	if err := isMCControllerDeployed(); err != nil {
		return err
	}

	workerNodeList, err := nodes.List(
		APIClient,
		metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()},
	)

	if err != nil {
		return err
	}

	glog.V(90).Infof("Verifying if cluster has enough workers to run MetalLb tests")

	if len(workerNodeList) < requiredWorkerNodeNumber {
		return fmt.Errorf("cluster has less than %d worker nodes", requiredWorkerNodeNumber)
	}

	return nil
}

func isMCControllerDeployed() error {
	metalLbNS := namespace.NewBuilder(APIClient, NetConfig.MlbOperatorNamespace)
	if !metalLbNS.Exists() {
		return fmt.Errorf("error metallb namespace %s doesn't exist", metalLbNS.Definition.Name)
	}

	metalLbController, err := deployment.Pull(
		APIClient, tsparams.OperatorControllerManager, NetConfig.MlbOperatorNamespace)

	if err != nil {
		return fmt.Errorf("error to pull metallb controller deployment %s from cluster", tsparams.OperatorControllerManager)
	}

	if !metalLbController.IsReady(30 * time.Second) {
		return fmt.Errorf("error metallb controller deployment %s is not in ready/ready state",
			tsparams.OperatorControllerManager)
	}

	return nil
}

// UpdateMachineConfigurationCluster adds nftables to cluster configuration.
func UpdateMachineConfigurationCluster(name, serviceName, servicePath string) error {
	//mcConfiguration, err := machineconfiguration.Pull(APIClient, "cluster")
	//
	//if err != nil {
	//	fmt.Println("There was an error pulling")
	//	return err
	//}
	//
	//abc := mcConfiguration.Object.Spec
	//
	//mcConfiguration.Object.Spec = machineconfigurationv1.MachineConfigurationSpec{}
	//mcConfiguration.Definition.Spec = machineconfigurationv1.MachineConfigurationSpec{}
	//
	//mcConfiguration.Object.Spec = machineconfigurationv1.MachineConfigurationSpec{
	//	NodeDisruptionPolicy: abc.NodeDisruptionPolicy,
	//}
	//mcConfiguration.Definition.Spec = machineconfigurationv1.MachineConfigurationSpec{
	//	NodeDisruptionPolicy: abc.NodeDisruptionPolicy,
	//}
	//mcConfiguration.Definition.Spec.ManagementState = machineconfigurationv1.Managed
	//mcConfiguration.Definition.Spec.ManagedBootImages = machineconfigurationv1.ManagedBootImages{
	//	MachineManagers: []machineconfigurationv1.MachineManager{
	//		{
	//			Resource: machineconfigurationv1.MachineSets,
	//			APIGroup: "machine.openshift.io",
	//			Selection: machineconfigurationv1.MachineManagerSelector{
	//				Mode: machineconfigurationv1.Partial,
	//				Partial: &machineconfigurationv1.PartialSelector{
	//					MachineResourceSelector: &metav1.LabelSelector{
	//						MatchLabels: map[string]string{"update-boot-image": "false"},
	//					},
	//				}},
	//		},
	//	},
	//}
	//
	//_, err = mcConfiguration.WithNodeDisruptionPolicy(serviceName, servicePath).Update(false)

	//if err != nil {
	//	return fmt.Errorf("error to update machineconfiguration cluster")
	//}

	return nil
}

func CreateButaneConfig(nodeRole string) []byte {
	butaneConfig := fmt.Sprintf(`variant: openshift
version: %s
metadata:
  name: 98-nftables-commatrix-%s
  labels:
    machineconfiguration.openshift.io/role: %s
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
          }`, butaneVersion, nodeRole, nodeRole)
	butaneConfig = strings.ReplaceAll(butaneConfig, "\t", "  ")
	return []byte(butaneConfig)

}

func CreateNftableCustomRule(nftRules []byte) *mco.MCBuilder {

	options := common.TranslateBytesOptions{}
	machineConfig, _, err := butaneConfig.TranslateBytes(nftRules, options)

	if err != nil {
		fmt.Println("something went wrong")
	}

	NftablesMachineConfigContents := `
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
	truePointer := true

	modeint := 384
	stringgzip := "gzip"
	filecontentsource := `
data:;base64,H4sIAAAAAAAC/3TMwUoDMRDG8XPyFB/1Cbwt9lTaFYpFl3WLB5ESk2k7GDNDnIKL9N2lBfG0x+//g8/CeyZwIUM8fZl87q7FJ8pkhCme6PjxLh4Dl796Hbv1Y7cdLuZsVMKes1HFUeQDXPRk0MpS2UbczqGSOY4IMZLa3Dt3g1UVxbDsoFINTdM0CCUhy+FyGRXpH7IcoJX2/I3Z6/a57e/Xffuy2Gze0C2WD+2AVf/U3WGGVEW9O/uz/w0AAP//kU0CJQUBAAA=`
	ignitionConfig := ignition.Config{
		Ignition: ignition.Ignition{
			Version: "3.1.0",
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
							Source:      &filecontentsource,
						},
						Mode: &modeint,
					},
				},
			},
		},
		Systemd: ignition.Systemd{
			Units: []ignition.Unit{
				{
					Enabled:  &truePointer,
					Name:     "nftables.service",
					Contents: &NftablesMachineConfigContents},
			},
		},
	}
	rawConfig, err := json.Marshal(ignitionConfig)
	Expect(err).ToNot(HaveOccurred(), "Failed to serialize ignition config")

	createdMC, err := mco.NewMCBuilder(APIClient, "98-nftables-cnf-worker").
		WithLabel("machineconfiguration.openshift.io/role", NetConfig.CnfMcpLabel).
		WithRawConfig(rawConfig).Create()

	Expect(err).ToNot(HaveOccurred(), "Failed to create nftables machine config")

	return createdMC
}

//func AddNFTSvcToNodeDisruptionPolicy() error {
//	machineConfigurationClient := mcopclientset.Interface
//	reloadApplyConfiguration := mcoac.ReloadService().WithServiceName("nftables.service")
//	restartApplyConfiguration := mcoac.RestartService().WithServiceName("nftables.service")
//
//	serviceName := "nftables.service"
//	serviceApplyConfiguration := mcoac.NodeDisruptionPolicySpecUnit().WithName(ocpoperatorv1.NodeDisruptionPolicyServiceName(serviceName)).WithActions(
//		mcoac.NodeDisruptionPolicySpecAction().WithType(ocpoperatorv1.ReloadSpecAction).WithReload(reloadApplyConfiguration),
//	)
//	fileApplyConfiguration := mcoac.NodeDisruptionPolicySpecFile().WithPath("/etc/sysconfig/nftables.conf").WithActions(
//		mcoac.NodeDisruptionPolicySpecAction().WithType(ocpoperatorv1.RestartSpecAction).WithRestart(restartApplyConfiguration),
//	)
//
//	applyConfiguration := mcoac.MachineConfiguration("cluster").WithSpec(mcoac.MachineConfigurationSpec().
//		WithManagementState("Managed").WithNodeDisruptionPolicy(mcoac.NodeDisruptionPolicyConfig().
//		WithUnits(serviceApplyConfiguration).WithFiles(fileApplyConfiguration)))
//
//	_, err := machineConfigurationClient.OperatorV1().MachineConfigurations().Apply(context.TODO(), applyConfiguration,
//		metav1.ApplyOptions{FieldManager: "machine-config-operator", Force: true})
//	//_, err := mcopclientset.Interface.OperatorV1().MachineConfigurations().Apply(context.TODO(), applyConfiguration,
//	//	metav1.ApplyOptions{FieldManager: "machine-config-operator", Force: true})
//
//	if err != nil {
//		return fmt.Errorf("updating cluster node disruption policy failed %v", err)
//	}
//
//	return nil
//}

//func dummy() {
//	machineconfiguration.NewMachineConfigurationBuilder()
//}

func indentContent(content string, indentSize int) string {
	lines := strings.Split(content, "\n")
	indent := strings.Repeat(" ", indentSize)
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}
