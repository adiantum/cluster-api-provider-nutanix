//go:build e2e

/*
Copyright 2025 Nutanix

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"

	infrav1 "github.com/nutanix-cloud-native/cluster-api-provider-nutanix/api/v1beta1"
)

var _ = Describe("Nutanix machine data disks", Label("capx-feature-test", "only-for-validation", "data-disks", "data-disk"), func() {
	const specName = "cluster-data-disks"

	var (
		namespace        *corev1.Namespace
		clusterName      string
		clusterResources *clusterctl.ApplyClusterTemplateAndWaitResult
		cancelWatches    context.CancelFunc
		testHelper       testHelperInterface
	)

	BeforeEach(func() {
		testHelper = newTestHelper(e2eConfig)
		clusterName = testHelper.generateTestClusterName(specName)
		clusterResources = new(clusterctl.ApplyClusterTemplateAndWaitResult)
		Expect(bootstrapClusterProxy).NotTo(BeNil(), "BootstrapCluterProxy can't be nil")
		namespace, cancelWatches = setupSpecNamespace(ctx, specName, bootstrapClusterProxy, artifactFolder)
	})

	AfterEach(func() {
		dumpSpecResourcesAndCleanup(ctx,
			specName, bootstrapClusterProxy,
			artifactFolder, namespace,
			cancelWatches, clusterResources.Cluster,
			e2eConfig.GetIntervals, skipCleanup)
	})

	It("Should create a cluster with data disks", func() {
		const flavor = "no-nmt"

		Expect(namespace).NotTo(BeNil(), "Namespace can't be nil")

		By("Creating a Nutanix Machine Config with data disks", func() {
			dataDiskNMT := testHelper.createDefaultNMTwithDataDisks(clusterName, namespace.Name, withDataDisksParams{
				DataDisks: []infrav1.NutanixMachineVMDisk{
					{
						DiskSize: resource.MustParse("10Gi"),
						DeviceProperties: &infrav1.NutanixMachineVMDiskDeviceProperties{
							DeviceType:  infrav1.NutanixMachineDiskDeviceTypeDisk,
							AdapterType: infrav1.NutanixMachineDiskAdapterTypeSCSI,
						},
					},
				},
			})

			testHelper.createCapiObject(ctx, createCapiObjectParams{
				creator:    bootstrapClusterProxy.GetClient(),
				capiObject: dataDiskNMT,
			})
		})

		By("Creating a workload cluster", func() {
			testHelper.deployCluster(deployClusterParams{
				clusterName:           clusterName,
				namespace:             namespace,
				flavor:                flavor,
				clusterctlConfigPath:  clusterctlConfigPath,
				artifactFolder:        artifactFolder,
				bootstrapClusterProxy: bootstrapClusterProxy,
			}, clusterResources)
		})

		By("Checking the data disks are attached to the VMs", func() {
			testHelper.verifyDisksOnNutanixMachines(ctx, verifyDisksOnNutanixMachinesParams{
				clusterName:           clusterName,
				namespace:             namespace.Name,
				bootstrapClusterProxy: bootstrapClusterProxy,
				diskCount:             2,
			})
		})
	})
})
