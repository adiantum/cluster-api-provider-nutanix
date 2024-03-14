package templates

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2/textlogger"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterctllog "sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/nutanix-cloud-native/cluster-api-provider-nutanix/api/v1beta1"
)

const (
	defaultNamespace = "default"
	kindClusterName  = "test-cluster"
)

var clnt client.Client

func init() {
	// Add NutanixCluster and NutanixMachine to the scheme
	_ = v1beta1.AddToScheme(scheme.Scheme)
	_ = capiv1.AddToScheme(scheme.Scheme)
	_ = apiextensionsv1.AddToScheme(scheme.Scheme)
	_ = controlplanev1.AddToScheme(scheme.Scheme)
}

func teardownTestEnvironment() error {
	provider := cluster.NewProvider(cluster.ProviderWithDocker())
	return provider.Delete(kindClusterName, "")
}

func setupTestEnvironment() (client.Client, error) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
	_ = teardownTestEnvironment()

	provider := cluster.NewProvider(cluster.ProviderWithDocker())
	err := provider.Create(kindClusterName, cluster.CreateWithNodeImage("kindest/node:v1.29.2"))
	if err != nil {
		return nil, fmt.Errorf("failed to create Kind cluster: %w", err)
	}

	kubeconfig, err := provider.KubeConfig(kindClusterName, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	tmpKubeconfig, err := os.CreateTemp("", "kubeconfig")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp kubeconfig: %w", err)
	}

	_, err = tmpKubeconfig.Write([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("failed to write to temp kubeconfig: %w", err)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("failed to get restconfig: %w", err)
	}

	clusterctllog.SetLogger(textlogger.NewLogger(textlogger.NewConfig()))
	clusterctl.Init(context.Background(), clusterctl.InitInput{
		KubeconfigPath:          tmpKubeconfig.Name(),
		InfrastructureProviders: []string{"nutanix:v1.4.0-alpha.1"},
		ClusterctlConfigPath:    "testdata/clusterctl-init.yaml",
	})

	clnt, err := client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	objects := getClusterClassObjects()
	for _, obj := range objects {
		if err := clnt.Create(context.Background(), obj); err != nil {
			fmt.Printf("failed to create object %s: %s\n\n", obj, err)
			continue
		}
	}

	return clnt, nil
}

func getObjectsFromYAML(filename string) ([]client.Object, error) {
	// read the file
	f, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Create a new YAML decoder
	decoder := yaml.NewYAMLToJSONDecoder(bytes.NewReader(f))

	// Decode the YAML manifests into client.Object instances
	var objects []client.Object
	for {
		var obj unstructured.Unstructured
		err := decoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode YAML: %w", err)
		}
		if obj.GetNamespace() == "" {
			obj.SetNamespace(defaultNamespace)
		}
		objects = append(objects, &obj)
	}

	return objects, nil
}

func getClusterClassObjects() []client.Object {
	const template = "cluster-template-clusterclass.yaml"
	objects, err := getObjectsFromYAML(template)
	if err != nil {
		return nil
	}

	return objects
}

func getClusterManifest(filePath string) (client.Object, error) {
	f, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var obj unstructured.Unstructured
	if err := yaml.NewYAMLToJSONDecoder(bytes.NewReader(f)).Decode(&obj); err != nil {
		return nil, fmt.Errorf("failed to decode YAML: %w", err)
	}

	obj.SetNamespace(defaultNamespace) // Set the namespace to default
	return &obj, nil
}

func fetchNutanixCluster(clnt client.Client, clusterName string) (*v1beta1.NutanixCluster, error) {
	nutanixClusterList := &v1beta1.NutanixClusterList{}
	if err := clnt.List(context.Background(), nutanixClusterList, &client.ListOptions{Namespace: defaultNamespace}); err != nil {
		return nil, fmt.Errorf("failed to list NutanixCluster: %w", err)
	}

	if len(nutanixClusterList.Items) == 0 {
		return nil, fmt.Errorf("no NutanixCluster found")
	}

	for _, nutanixCluster := range nutanixClusterList.Items {
		if strings.Contains(nutanixCluster.Name, clusterName) {
			return &nutanixCluster, nil
		}
	}

	return nil, fmt.Errorf("matching NutanixCluster not found")
}

func fetchMachineTemplates(clnt client.Client, clusterName string) ([]*v1beta1.NutanixMachineTemplate, error) {
	nutanixMachineTemplateList := &v1beta1.NutanixMachineTemplateList{}
	if err := clnt.List(context.Background(), nutanixMachineTemplateList, &client.ListOptions{Namespace: defaultNamespace}); err != nil {
		return nil, fmt.Errorf("failed to list NutanixMachine: %w", err)
	}

	if len(nutanixMachineTemplateList.Items) == 0 {
		return nil, fmt.Errorf("no NutanixMachine found")
	}

	nmts := make([]*v1beta1.NutanixMachineTemplate, 0)
	for _, nmt := range nutanixMachineTemplateList.Items {
		if nmt.ObjectMeta.Labels[capiv1.ClusterNameLabel] == clusterName {
			nmts = append(nmts, &nmt)
		}
	}

	if len(nmts) == 0 {
		return nil, fmt.Errorf("no NutanixMachineTemplate found for cluster %s", clusterName)
	}

	return nmts, nil
}

func fetchKubeadmControlPlane(clnt client.Client, clusterName string) (*controlplanev1.KubeadmControlPlane, error) {
	kubeadmControlPlaneList := &controlplanev1.KubeadmControlPlaneList{}
	if err := clnt.List(context.Background(), kubeadmControlPlaneList, &client.ListOptions{Namespace: defaultNamespace}); err != nil {
		return nil, fmt.Errorf("failed to list KubeadmControlPlane: %w", err)
	}

	for _, kcp := range kubeadmControlPlaneList.Items {
		if kcp.ObjectMeta.Labels[capiv1.ClusterNameLabel] == clusterName {
			return &kcp, nil
		}
	}

	return nil, fmt.Errorf("no KubeadmControlPlane found for cluster %s", clusterName)
}

func fetchControlPlaneMachineTemplate(clnt client.Client, clusterName string) (*v1beta1.NutanixMachineTemplate, error) {
	nmts, err := fetchMachineTemplates(clnt, clusterName)
	if err != nil {
		return nil, err
	}

	kcp, err := fetchKubeadmControlPlane(clnt, clusterName)
	if err != nil {
		return nil, err
	}

	for _, nmt := range nmts {
		if nmt.ObjectMeta.Name == kcp.Spec.MachineTemplate.InfrastructureRef.Name {
			return nmt, nil
		}
	}

	return nil, fmt.Errorf("no control plane NutanixMachineTemplate found for cluster %s", clusterName)
}

func TestClusterClassTemplateSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	BeforeSuite(func() {
		c, err := setupTestEnvironment()
		Expect(err).NotTo(HaveOccurred())
		clnt = c
	})
	AfterSuite(func() {
		Expect(teardownTestEnvironment()).NotTo(HaveOccurred())
	})

	RunSpecs(t, "Template Tests Suite")
}

var _ = Describe("Cluster Class Template Patches Test Suite", Ordered, func() {
	Describe("patches for failure domains", Ordered, func() {
		var obj client.Object
		It("should create the cluster with failure domains", func() {
			clusterManifest := "testdata/cluster-with-failure-domain.yaml"
			o, err := getClusterManifest(clusterManifest)
			Expect(err).NotTo(HaveOccurred())
			obj = o

			Expect(clnt.Create(context.Background(), obj)).NotTo(HaveOccurred())
		})

		It("NutanixCluster should have correct failure domains", func() {
			Eventually(func() (*v1beta1.NutanixCluster, error) {
				return fetchNutanixCluster(clnt, obj.GetName())
			}).Within(time.Minute).Should(And(
				HaveField("Spec.FailureDomains", HaveLen(3)),
				HaveField("Spec.FailureDomains", HaveEach(HaveField("Name", BeAssignableToTypeOf("string")))),
				HaveField("Spec.FailureDomains", HaveEach(HaveField("ControlPlane", HaveValue(Equal(true))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Cluster.Type", HaveValue(Equal(v1beta1.NutanixIdentifierUUID))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Cluster.UUID", HaveValue(Equal("00000000-0000-0000-0000-000000000001"))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Cluster.Type", HaveValue(Equal(v1beta1.NutanixIdentifierName))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Cluster.Name", HaveValue(Equal("cluster1"))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Cluster.Name", HaveValue(Equal("cluster2"))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Subnets", ContainElement(HaveField("Type", HaveValue(Equal(v1beta1.NutanixIdentifierUUID))))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Subnets", ContainElement(HaveField("Name", HaveValue(Equal("subnet1"))))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Subnets", ContainElement(HaveField("Name", HaveValue(Equal("subnet2"))))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Subnets", ContainElement(HaveField("Type", HaveValue(Equal(v1beta1.NutanixIdentifierName))))))),
				HaveField("Spec.FailureDomains", ContainElement(HaveField("Subnets", ContainElement(HaveField("UUID", HaveValue(Equal("00000000-0000-0000-0000-000000000001"))))))),
			))
		})

		It("Control Plane NutanixMachineTemplate should not have the cluster and subnets set", func() {
			Eventually(func() (*v1beta1.NutanixMachineTemplate, error) {
				return fetchControlPlaneMachineTemplate(clnt, obj.GetName())
			}).Within(time.Minute).Should(And(
				HaveField("Spec.Template.Spec.Cluster", BeZero()),
				HaveField("Spec.Template.Spec.Subnets", HaveLen(0))))
		})

		It("should delete the cluster", func() {
			Expect(clnt.Delete(context.Background(), obj)).NotTo(HaveOccurred())
		})
	})
})