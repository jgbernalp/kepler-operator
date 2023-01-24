package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"testing"

	"github.com/go-logr/logr"
	securityv1 "github.com/openshift/api/security/v1"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	monitoring "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	keplersystemv1alpha1 "github.com/sustainable.computing.io/kepler-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	//"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	DaemonSetName                          = KeplerOperatorName + "-exporter"
	ServiceName                            = KeplerOperatorName + "-exporter"
	KeplerOperatorName                     = "kepler"
	KeplerOperatorNameSpace                = ""
	ServiceAccountName                     = KeplerOperatorName + "-sa"
	ServiceAccountNameSpace                = KeplerOperatorNameSpace
	ClusterRoleName                        = "kepler-clusterrole"
	ClusterRoleNameSpace                   = ""
	ClusterRoleBindingName                 = "kepler-clusterrole-binding"
	ClusterRoleBindingNameSpace            = ""
	DaemonSetNameSpace                     = KeplerOperatorNameSpace
	ServiceNameSpace                       = KeplerOperatorNameSpace
	CollectorConfigMapName                 = KeplerOperatorName + "-exporter-cfm"
	CollectorConfigMapNameSpace            = KeplerOperatorNameSpace
	SCCObjectName                          = "kepler-scc"
	SCCObjectNameSpace                     = KeplerOperatorNameSpace
	MachineConfigCGroupKernelArgMasterName = "50-master-cgroupv2"
	MachineConfigCGroupKernelArgWorkerName = "50-worker-cgroupv2"
	MachineConfigDevelMasterName           = "51-master-kernel-devel"
	MachineConfigDevelWorkerName           = "51-worker-kernel-devel"
)

func generateDefaultOperatorSettings() (context.Context, *KeplerReconciler, *keplersystemv1alpha1.Kepler, logr.Logger, client.Client) {
	ctx := context.Background()
	_ = log.FromContext(ctx)
	logger := log.Log.WithValues("kepler", types.NamespacedName{Name: "kepler-operator", Namespace: "kepler"})

	keplerInstance := &keplersystemv1alpha1.Kepler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KeplerOperatorName,
			Namespace: KeplerOperatorNameSpace,
		},
		Spec: keplersystemv1alpha1.KeplerSpec{
			Collector: &keplersystemv1alpha1.CollectorSpec{
				Image: "quay.io/sustainable_computing_io/kepler:latest",
			},
		},
	}

	keplerobjs := []runtime.Object{keplerInstance}

	s := scheme.Scheme
	s.AddKnownTypes(keplersystemv1alpha1.SchemeBuilder.GroupVersion, keplerInstance)

	clientBuilder := fake.NewClientBuilder()
	clientBuilder = clientBuilder.WithRuntimeObjects(keplerobjs...)
	clientBuilder = clientBuilder.WithScheme(s)
	cl := clientBuilder.Build()
	monitoring.AddToScheme(s)
	mcfgv1.AddToScheme(s)
	securityv1.AddToScheme(s)
	keplerReconciler := &KeplerReconciler{Client: cl, Scheme: s, Log: logger}

	return ctx, keplerReconciler, keplerInstance, logger, cl
}

func CheckSetControllerReference(OwnerName string, OwnerKind string, obj client.Object) bool {
	for _, ownerReference := range obj.GetOwnerReferences() {
		if ownerReference.Name == OwnerName && ownerReference.Kind == OwnerKind {
			//owner has been set properly
			return true
		}
	}
	return false
}

func testVerifyMainReconciler(t *testing.T, ctx context.Context, client client.Client) {
	//Check Kepler Instance has been updated as desired
	foundKepler := &keplersystemv1alpha1.Kepler{}
	foundKeplerError := client.Get(ctx, types.NamespacedName{Name: KeplerOperatorName, Namespace: KeplerOperatorNameSpace}, foundKepler)
	if foundKeplerError != nil {
		t.Fatalf("Kepler Instance was not created: (%v)", foundKeplerError)
	}
	assert.Equal(t, keplersystemv1alpha1.ConditionReconciled, foundKepler.Status.Conditions.Type)
	assert.Equal(t, "Reconcile complete", foundKepler.Status.Conditions.Message)
	assert.Equal(t, keplersystemv1alpha1.ReconciledReasonComplete, foundKepler.Status.Conditions.Reason)

	//Verify Sub-Reconcilers
	testVerifyCollectorReconciler(t, ctx, client)
}

func testVerifyCollectorReconciler(t *testing.T, ctx context.Context, client client.Client) {
	//Verify mock client objects exist
	foundServiceAccount := &corev1.ServiceAccount{}
	foundClusterRole := &rbacv1.ClusterRole{}
	foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	serviceAccountError := client.Get(ctx, types.NamespacedName{Name: ServiceAccountName, Namespace: ServiceAccountNameSpace}, foundServiceAccount)
	if serviceAccountError != nil {
		t.Fatalf("service account was not stored: (%v)", serviceAccountError)
	}
	clusterRoleError := client.Get(ctx, types.NamespacedName{Name: ClusterRoleName, Namespace: ClusterRoleNameSpace}, foundClusterRole)
	if clusterRoleError != nil {
		t.Fatalf("cluster role was not stored: (%v)", clusterRoleError)
	}
	clusterRoleBindingError := client.Get(ctx, types.NamespacedName{Name: ClusterRoleBindingName, Namespace: ClusterRoleBindingNameSpace}, foundClusterRoleBinding)
	if clusterRoleBindingError != nil {
		t.Fatalf("cluster role binding was not stored: (%v)", clusterRoleBindingError)
	}

	foundService := &corev1.Service{}
	serviceError := client.Get(ctx, types.NamespacedName{Name: ServiceName, Namespace: KeplerOperatorNameSpace}, foundService)

	if serviceError != nil {
		t.Fatalf("service was not stored: (%v)", serviceError)
	}
	foundDaemonSet := &appsv1.DaemonSet{}
	daemonSetError := client.Get(ctx, types.NamespacedName{Name: DaemonSetName, Namespace: KeplerOperatorNameSpace}, foundDaemonSet)
	if daemonSetError != nil {
		t.Fatalf("daemon Object was not stored: (%v)", daemonSetError)
	}

	foundConfigMap := &corev1.ConfigMap{}
	configMapError := client.Get(ctx, types.NamespacedName{Name: CollectorConfigMapName, Namespace: CollectorConfigMapNameSpace}, foundConfigMap)
	if configMapError != nil {
		t.Fatalf("config map was not stored: (%v)", configMapError)
	}
	foundSCC := &securityv1.SecurityContextConstraints{}
	sccError := client.Get(ctx, types.NamespacedName{Name: SCCObjectName, Namespace: SCCObjectNameSpace}, foundSCC)
	if sccError != nil {
		if strings.Contains(sccError.Error(), "no matches for kind") {
			fmt.Printf("resulting error not a timeout: %s", sccError)
		} else {
			t.Fatalf("scc was not stored: (%v)", sccError)
		}
	} else {
		testVerifySCC(t, *foundSCC)
	}

	foundMasterCgroupKernelArgs := &mcfgv1.MachineConfig{}
	foundWorkerCgroupKernelArgs := &mcfgv1.MachineConfig{}
	foundMasterDevel := &mcfgv1.MachineConfig{}
	foundWorkerDevel := &mcfgv1.MachineConfig{}
	masterCgroupKernelArgsError := client.Get(ctx, types.NamespacedName{Name: MachineConfigCGroupKernelArgMasterName, Namespace: ""}, foundMasterCgroupKernelArgs)
	workerCgroupKernelArgsError := client.Get(ctx, types.NamespacedName{Name: MachineConfigCGroupKernelArgWorkerName, Namespace: ""}, foundWorkerCgroupKernelArgs)
	masterDevelError := client.Get(ctx, types.NamespacedName{Name: MachineConfigDevelMasterName, Namespace: ""}, foundMasterDevel)
	workerDevelError := client.Get(ctx, types.NamespacedName{Name: MachineConfigDevelWorkerName, Namespace: ""}, foundWorkerDevel)

	if masterCgroupKernelArgsError != nil && strings.Contains(masterCgroupKernelArgsError.Error(), "no matches for kind") {
		fmt.Printf("resulting error not a timeout: %s", masterCgroupKernelArgsError)
	} else {
		if masterCgroupKernelArgsError != nil {
			t.Fatalf("cgroup kernel arguments master machine config has not been stored: (%v)", masterCgroupKernelArgsError)
		}
		if workerCgroupKernelArgsError != nil {
			t.Fatalf("cgroup kernel arguments worker machine config has not been stored: (%v)", workerCgroupKernelArgsError)
		}

		if masterDevelError != nil {
			t.Fatalf("devel master machine config has not been stored: (%v)", masterDevelError)
		}

		if workerDevelError != nil {
			t.Fatalf("devel worker machine config has not been stored: (%v)", workerDevelError)
		}

		testVerifyBasicMachineConfig(t, *foundMasterCgroupKernelArgs, *foundWorkerCgroupKernelArgs, *foundMasterDevel, *foundWorkerDevel)
	}

	testVerifyServiceAccountSpec(t, *foundServiceAccount, *foundClusterRole, *foundClusterRoleBinding)
	testVerifyServiceSpec(t, *foundService)
	//Note testVerifyDaemonSpec already ensures SA is assigned to Daemonset
	testVerifyDaemonSpec(t, *foundServiceAccount, *foundDaemonSet)
	testVerifyConfigMap(t, *foundConfigMap)

	//Verify Collector related cross object relationships are valid

	//Verify ServiceAccount Specified in DaemonSet
	assert.Equal(t, foundServiceAccount.Name, foundDaemonSet.Spec.Template.Spec.ServiceAccountName)

	//Verify Service selector matches daemonset spec template labels
	//Service Selector must exist correctly to connect to daemonset
	// Service Selector or SCC Labels is subset of MatchLabels and Labels in Daemonset (DaemonSet MatchLbels and Labels should be superset)
	for key, value := range foundService.Spec.Selector {
		assert.Contains(t, foundDaemonSet.Spec.Template.ObjectMeta.Labels, key)
		assert.Equal(t, value, foundDaemonSet.Spec.Template.ObjectMeta.Labels[key])
	}
	//Verify SCC Labels and Daemonset correspond
	for key, value := range foundSCC.ObjectMeta.Labels {
		assert.Contains(t, foundDaemonSet.Spec.Template.ObjectMeta.Labels, key)
		assert.Equal(t, value, foundDaemonSet.Spec.Template.ObjectMeta.Labels[key])
	}
	//Verify SCC User includes Kepler
	assert.Contains(t, foundSCC.Users, KeplerOperatorName)
	//Verify SCC User includes Kepler's Service Account
	for _, user := range foundSCC.Users {
		if strings.Contains(user, "system:serviceaccount:") {
			assert.Equal(t, "system:serviceaccount:"+ServiceAccountNameSpace+":"+ServiceAccountName, user)
		}
	}
	//Verify ConfigMap exists in Daemonset Volumes
	encounteredConfigMapVolume := false
	for _, volume := range foundDaemonSet.Spec.Template.Spec.Volumes {
		if volume.VolumeSource.ConfigMap != nil {
			//found configmap
			if foundConfigMap.ObjectMeta.Name == volume.VolumeSource.ConfigMap.Name {
				encounteredConfigMapVolume = true
			}
		}
	}
	assert.True(t, encounteredConfigMapVolume)

}

func testVerifySCC(t *testing.T, returnedSCC securityv1.SecurityContextConstraints) {
	// ensure some basic, desired settings are in place
	assert.NotEmpty(t, returnedSCC.ObjectMeta.Labels)
	assert.True(t, returnedSCC.AllowPrivilegedContainer)
	assert.Equal(t, securityv1.FSGroupStrategyOptions{
		Type: securityv1.FSGroupStrategyRunAsAny,
	}, returnedSCC.FSGroup)
	assert.Equal(t, securityv1.SELinuxContextStrategyOptions{
		Type: securityv1.SELinuxStrategyRunAsAny,
	},
		returnedSCC.SELinuxContext)
	assert.Equal(t, securityv1.RunAsUserStrategyOptions{
		Type: securityv1.RunAsUserStrategyRunAsAny,
	},
		returnedSCC.RunAsUser)

}

func testVerifyBasicMachineConfig(t *testing.T, cgroupMasterMC mcfgv1.MachineConfig, cgroupWorkerMC mcfgv1.MachineConfig, develMasterMC mcfgv1.MachineConfig, develWorkerMC mcfgv1.MachineConfig) {
	// check if all relevant Machine Config Features have been deployed correctly

	assert.NotEmpty(t, cgroupMasterMC.Labels)
	assert.Contains(t, cgroupMasterMC.Labels, "machineconfiguration.openshift.io/role")
	assert.Equal(t, "master", cgroupMasterMC.Labels["machineconfiguration.openshift.io/role"])

	assert.NotEmpty(t, cgroupWorkerMC.Labels)
	assert.Contains(t, cgroupWorkerMC.Labels, "machineconfiguration.openshift.io/role")
	assert.Equal(t, "worker", cgroupWorkerMC.Labels["machineconfiguration.openshift.io/role"])

	assert.NotEmpty(t, develMasterMC.Labels)
	assert.Contains(t, develMasterMC.Labels, "machineconfiguration.openshift.io/role")
	assert.Equal(t, "master", develMasterMC.Labels["machineconfiguration.openshift.io/role"])

	assert.NotEmpty(t, develWorkerMC.Labels)
	assert.Contains(t, develWorkerMC.Labels, "machineconfiguration.openshift.io/role")
	assert.Equal(t, "worker", develWorkerMC.Labels["machineconfiguration.openshift.io/role"])

	// check if all relevant Machine Config Objects have correct spec
	assert.NotEmpty(t, develMasterMC.Spec)
	assert.NotEmpty(t, develWorkerMC.Spec)
	assert.NotEmpty(t, cgroupMasterMC.Spec)
	assert.NotEmpty(t, cgroupWorkerMC.Spec)

	assert.NotEmpty(t, develMasterMC.Spec.Extensions)
	assert.Contains(t, develMasterMC.Spec.Extensions, "kernel-devel")

	assert.NotEmpty(t, develWorkerMC.Spec.Extensions)
	assert.Contains(t, develWorkerMC.Spec.Extensions, "kernel-devel")

	assert.NotEmpty(t, cgroupMasterMC.Spec.KernelArguments)
	assert.Contains(t, cgroupMasterMC.Spec.KernelArguments, "systemd.unified_cgroup_hierarchy=1")
	assert.Contains(t, cgroupMasterMC.Spec.KernelArguments, "cgroup_no_v1='all'")

	assert.NotEmpty(t, cgroupWorkerMC.Spec.KernelArguments)
	assert.Contains(t, cgroupWorkerMC.Spec.KernelArguments, "systemd.unified_cgroup_hierarchy=1")
	assert.Contains(t, cgroupWorkerMC.Spec.KernelArguments, "cgroup_no_v1='all'")
}

func testVerifyConfigMap(t *testing.T, returnedConfigMap corev1.ConfigMap) {
	// check SetControllerReference has been set (all objects require owners) properly
	result := CheckSetControllerReference(KeplerOperatorName, "Kepler", &returnedConfigMap)
	if !result {
		t.Fatalf("failed to set controller reference: config map")
	}
	//check if ConfigMap contains proper datamap
	assert.NotEmpty(t, returnedConfigMap.Data)
	assert.Equal(t, KeplerOperatorNameSpace, returnedConfigMap.Data["KEPLER_NAMESPACE"])
}

func testVerifyServiceSpec(t *testing.T, returnedService corev1.Service) {
	// check SetControllerReference has been set (all objects require owners) properly
	result := CheckSetControllerReference(KeplerOperatorName, "Kepler", &returnedService)
	if !result {
		t.Fatalf("failed to set controller reference: service")
	}
	//check if CreateOrUpdate Object has properly set up required fields, nested fields, and variable fields for SA
	assert.NotEmpty(t, returnedService.ObjectMeta)
	assert.Equal(t, ServiceName, returnedService.ObjectMeta.Name)
	assert.Equal(t, ServiceNameSpace, returnedService.ObjectMeta.Namespace)
	assert.NotEmpty(t, returnedService.Spec)
	assert.NotEmpty(t, returnedService.Spec.Ports)
	assert.NotEmpty(t, returnedService.Spec.Selector)
	assert.Equal(t, "None", returnedService.Spec.ClusterIP)

}

func testVerifyServiceAccountSpec(t *testing.T, returnedServiceAccount corev1.ServiceAccount, returnedClusterRole rbacv1.ClusterRole, returnedClusterRoleBinding rbacv1.ClusterRoleBinding) {
	// check SetControllerReference has been set (all objects require owners) properly for SA, Role, RoleBinding
	result := CheckSetControllerReference(KeplerOperatorName, "Kepler", &returnedServiceAccount)
	if !result {
		t.Fatalf("failed to set controller reference: service account")
	}

	//assert.Equal(t, 1, len(returnedServiceAccount.GetOwnerReferences()))
	//assert.Equal(t, 1, len(returnedRole.GetOwnerReferences()))
	//assert.Equal(t, 1, len(returnedRoleBinding.GetOwnerReferences()))
	//assert.Equal(t, KeplerOperatorName, returnedServiceAccount.GetOwnerReferences()[0].Name)
	//assert.Equal(t, KeplerOperatorName, returnedRole.GetOwnerReferences()[0].Name)
	//assert.Equal(t, KeplerOperatorName, returnedRoleBinding.GetOwnerReferences()[0].Name)

	//assert.Equal(t, "Kepler", returnedServiceAccount.GetOwnerReferences()[0].Kind)
	//assert.Equal(t, "Kepler", returnedRole.GetOwnerReferences()[0].Kind)
	//assert.Equal(t, "Kepler", returnedRoleBinding.GetOwnerReferences()[0].Kind)

	//check if CreateOrUpdate Object has properly set up required fields, nested fields, and variable fields for SA
	assert.NotEmpty(t, returnedServiceAccount.ObjectMeta)
	assert.Equal(t, ServiceAccountName, returnedServiceAccount.ObjectMeta.Name)
	assert.Equal(t, ServiceAccountNameSpace, returnedServiceAccount.ObjectMeta.Namespace)

	//check if CreateOrUpdate Object has properly set up required fields, nested fields, and variable fields for ClusterRole
	assert.NotEmpty(t, returnedClusterRole.ObjectMeta)
	assert.Equal(t, ClusterRoleName, returnedClusterRole.ObjectMeta.Name)
	assert.Equal(t, ClusterRoleNameSpace, returnedClusterRole.ObjectMeta.Namespace)
	assert.NotEmpty(t, returnedClusterRole.Rules)

	//check if CreateOrUpdate Object has properly set up required fields, nested fields, and variable fields for ClusterRoleBinding
	assert.NotEmpty(t, returnedClusterRoleBinding.ObjectMeta)
	assert.Equal(t, ClusterRoleBindingName, returnedClusterRoleBinding.ObjectMeta.Name)
	assert.Equal(t, ClusterRoleBindingNameSpace, returnedClusterRoleBinding.ObjectMeta.Namespace)
	assert.NotEmpty(t, returnedClusterRoleBinding.RoleRef)
	assert.NotEmpty(t, returnedClusterRoleBinding.Subjects)
	assert.Equal(t, returnedServiceAccount.Kind, returnedClusterRoleBinding.Subjects[0].Kind)
	assert.Equal(t, returnedServiceAccount.Name, returnedClusterRoleBinding.Subjects[0].Name)
	assert.Equal(t, returnedServiceAccount.Namespace, returnedClusterRoleBinding.Subjects[0].Namespace)
	assert.Equal(t, "rbac.authorization.k8s.io", returnedClusterRoleBinding.RoleRef.APIGroup)
	assert.Equal(t, returnedClusterRole.Kind, returnedClusterRoleBinding.RoleRef.Kind)
	assert.Equal(t, returnedClusterRole.Name, returnedClusterRoleBinding.RoleRef.Name)

}

func testVerifyDaemonSpec(t *testing.T, returnedServiceAccount corev1.ServiceAccount, returnedDaemonSet appsv1.DaemonSet) {
	// check SetControllerReference has been set (all objects require owners) properly
	result := CheckSetControllerReference(KeplerOperatorName, "Kepler", &returnedDaemonSet)
	if !result {
		t.Fatalf("failed to set controller reference: daemonset")
	}
	// check if CreateOrUpdate Object has properly set up required fields, nested fields, and variable fields
	assert.NotEmpty(t, returnedDaemonSet.Spec)
	assert.NotEmpty(t, returnedDaemonSet.Spec.Template)
	assert.NotEmpty(t, returnedDaemonSet.Spec.Template.Spec)
	assert.NotEmpty(t, returnedDaemonSet.ObjectMeta)
	assert.NotEmpty(t, returnedDaemonSet.Spec.Template.ObjectMeta)

	assert.NotEqual(t, 0, len(returnedDaemonSet.Spec.Template.Spec.Containers))

	for _, container := range returnedDaemonSet.Spec.Template.Spec.Containers {
		//check security
		if container.Name == "kepler-exporter" {
			assert.True(t, *container.SecurityContext.Privileged)
		}
		assert.NotEmpty(t, container.Image)
		assert.NotEmpty(t, container.Name)

	}

	assert.Equal(t, DaemonSetName, returnedDaemonSet.ObjectMeta.Name)
	assert.Equal(t, DaemonSetNameSpace, returnedDaemonSet.ObjectMeta.Namespace)

	assert.Equal(t, DaemonSetName, returnedDaemonSet.Spec.Template.ObjectMeta.Name)
	assert.True(t, returnedDaemonSet.Spec.Template.Spec.HostNetwork)
	assert.Equal(t, returnedServiceAccount.Name, returnedDaemonSet.Spec.Template.Spec.ServiceAccountName)
	// check if daemonset obeys general rules
	//TODO: MATCH LABELS IS subset to labels. SAME WITH SELECTOR IN SERVICE
	// NEED TO MAKE SURE RELATED SERVICE CONNECTS TO EXISTING LABELS IN DAEMONSET PODS TOO
	for key, value := range returnedDaemonSet.Spec.Selector.MatchLabels {
		assert.Contains(t, returnedDaemonSet.Spec.Template.ObjectMeta.Labels, key)
		assert.Equal(t, value, returnedDaemonSet.Spec.Template.ObjectMeta.Labels[key])
	}

	assert.Equal(t, returnedDaemonSet.Spec.Selector.MatchLabels, returnedDaemonSet.Spec.Template.ObjectMeta.Labels)

	if returnedDaemonSet.Spec.Template.Spec.RestartPolicy != "" {
		assert.Equal(t, corev1.RestartPolicyAlways, returnedDaemonSet.Spec.Template.Spec.RestartPolicy)
	}
	for _, container := range returnedDaemonSet.Spec.Template.Spec.Containers {

		for _, port := range container.Ports {
			assert.NotEmpty(t, port.ContainerPort)
			assert.Less(t, port.ContainerPort, int32(65536))
			assert.Greater(t, port.ContainerPort, int32(0))
		}

	}
	//check that probe ports correspond to an existing containe port
	// currently we assume the probe ports are integers and we only use integer ports (no referencing ports by name)
	for _, container := range returnedDaemonSet.Spec.Template.Spec.Containers {
		if container.LivenessProbe != nil {
			assert.NotEmpty(t, container.LivenessProbe.ProbeHandler)
			encountered := false
			for _, port := range container.Ports {
				if container.LivenessProbe.HTTPGet != nil {
					assert.NotEmpty(t, container.LivenessProbe.HTTPGet.Port)
					if port.ContainerPort == int32(container.LivenessProbe.HTTPGet.Port.IntValue()) {
						encountered = true
					}
				} else if container.LivenessProbe.TCPSocket != nil {
					assert.NotEmpty(t, container.LivenessProbe.TCPSocket.Port)
					if port.ContainerPort == int32(container.LivenessProbe.TCPSocket.Port.IntValue()) {
						encountered = true
					}
				} else if container.LivenessProbe.Exec != nil {
					//TODO: Include Checks
				}
			}
			assert.True(t, encountered)
		}
		//not in use
		if container.ReadinessProbe != nil {
			assert.NotEmpty(t, container.ReadinessProbe.ProbeHandler)
			encountered := false
			for _, port := range container.Ports {
				if container.ReadinessProbe.HTTPGet != nil {
					assert.NotEmpty(t, container.ReadinessProbe.HTTPGet.Port)
					if port.ContainerPort == int32(container.ReadinessProbe.HTTPGet.Port.IntValue()) {
						encountered = true
					}
				} else if container.ReadinessProbe.TCPSocket != nil {
					assert.NotEmpty(t, container.ReadinessProbe.TCPSocket.Port)
					if port.ContainerPort == int32(container.ReadinessProbe.TCPSocket.Port.IntValue()) {
						encountered = true
					}
				} else if container.ReadinessProbe.Exec != nil {
					//TODO: Include Checks
				}
			}
			assert.True(t, encountered)

		}
		//not in use
		if container.StartupProbe != nil {
			assert.NotEmpty(t, container.StartupProbe.ProbeHandler)
			encountered := false
			for _, port := range container.Ports {
				if container.StartupProbe.HTTPGet != nil {
					assert.NotEmpty(t, container.StartupProbe.HTTPGet.Port)
					if port.ContainerPort == int32(container.StartupProbe.HTTPGet.Port.IntValue()) {
						encountered = true
					}
				} else if container.StartupProbe.TCPSocket != nil {
					assert.NotEmpty(t, container.StartupProbe.TCPSocket.Port)
					if port.ContainerPort == int32(container.StartupProbe.TCPSocket.Port.IntValue()) {
						encountered = true
					}
				} else if container.StartupProbe.Exec != nil {
					//TODO: Include Checks
				}
			}
			assert.True(t, encountered)
		}
	}

	// ensure volumemounts reference existing volumes
	volumes := returnedDaemonSet.Spec.Template.Spec.Volumes
	//TODO: note that volumes that are not mounted is not allowed. Is this worth addressing?
	for _, container := range returnedDaemonSet.Spec.Template.Spec.Containers {
		encountered := false
		for _, volumeMount := range container.VolumeMounts {
			for _, volume := range volumes {
				if volumeMount.Name == volume.Name { //&& volumeMount.MountPath == volume.VolumeSource.HostPath.Path {
					encountered = true
				}
			}
		}
		assert.True(t, encountered)
	}

}

func TestEnsureKeplerOperator(t *testing.T) {
	ctx, keplerReconciler, _, _, client := generateDefaultOperatorSettings()
	r := keplerReconciler
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      KeplerOperatorName,
			Namespace: KeplerOperatorNameSpace,
		},
	}
	//should only call reconcile once (Additional reconciliations will be called if requeing is required)
	res, err := r.Reconcile(ctx, req)
	//continue reconcoiling until requeue has been terminated accordingly
	for timeout := time.After(30 * time.Second); res.Requeue; {
		select {
		case <-timeout:
			t.Fatalf("main reconciler never terminates")
		default:
		}
		res, err = r.Reconcile(ctx, req)
	}
	//once reconciling has terminated accordingly, perform expected tests
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	testVerifyMainReconciler(t, ctx, client)

}

func TestEnsureDaemon(t *testing.T) {
	ctx, keplerReconciler, keplerInstance, logger, client := generateDefaultOperatorSettings()
	r := collectorReconciler{
		KeplerReconciler: *keplerReconciler,
		Instance:         keplerInstance,
		Ctx:              ctx,
	}
	r.serviceAccount = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KeplerOperatorName,
			Namespace: KeplerOperatorNameSpace,
		},
	}

	res, err := r.ensureDaemonSet(logger)
	//basic check
	assert.True(t, res)
	if err != nil {
		t.Fatalf("daemonset has failed which should not happen: (%v)", err)
	}
	foundDaemonSet := &appsv1.DaemonSet{}
	daemonSetError := client.Get(ctx, types.NamespacedName{Name: DaemonSetName, Namespace: DaemonSetNameSpace}, foundDaemonSet)

	if daemonSetError != nil {
		t.Fatalf("daemonset has not been stored: (%v)", daemonSetError)
	}

	testVerifyDaemonSpec(t, *r.serviceAccount, *foundDaemonSet)

	r = collectorReconciler{
		Ctx:              ctx,
		Instance:         keplerInstance,
		KeplerReconciler: *keplerReconciler,
		serviceAccount: &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "random",
				Namespace: "random_two",
			},
		},
	}

	res, err = r.ensureDaemonSet(logger)
	//basic check
	assert.True(t, res)
	if err != nil {
		t.Fatalf("daemonset has failed which should not happen: (%v)", err)
	}
	foundDaemonSet = &appsv1.DaemonSet{}
	daemonSetError = client.Get(ctx, types.NamespacedName{Name: DaemonSetName, Namespace: KeplerOperatorNameSpace}, foundDaemonSet)
	if daemonSetError != nil {
		t.Fatalf("daemonset has not been stored: (%v)", daemonSetError)
	}

	testVerifyDaemonSpec(t, *r.serviceAccount, *foundDaemonSet)

}

func TestEnsureServiceAccount(t *testing.T) {
	ctx, keplerReconciler, keplerInstance, logger, client := generateDefaultOperatorSettings()
	r := collectorReconciler{
		KeplerReconciler: *keplerReconciler,
		Instance:         keplerInstance,
		Ctx:              ctx,
	}
	numOfReconciliations := 3
	for i := 0; i < numOfReconciliations; i++ {
		//should also affect role and role binding
		res, err := r.ensureServiceAccount(logger)
		//basic check
		assert.True(t, res)
		if err != nil {
			t.Fatalf("service account reconciler has failed which should not happen: (%v)", err)
		}
		foundServiceAccount := &corev1.ServiceAccount{}
		serviceAccountError := client.Get(ctx, types.NamespacedName{Name: ServiceAccountName, Namespace: ServiceAccountNameSpace}, foundServiceAccount)
		foundClusterRole := &rbacv1.ClusterRole{}
		clusterRoleError := client.Get(ctx, types.NamespacedName{Name: ClusterRoleName, Namespace: ClusterRoleNameSpace}, foundClusterRole)
		foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
		clusterRoleBindingError := client.Get(ctx, types.NamespacedName{Name: ClusterRoleBindingName, Namespace: ClusterRoleBindingNameSpace}, foundClusterRoleBinding)

		if serviceAccountError != nil {
			t.Fatalf("service account has not been stored: (%v)", serviceAccountError)
		}
		if clusterRoleError != nil {
			t.Fatalf("cluster role has not been stored: (%v)", clusterRoleError)
		}
		if clusterRoleBindingError != nil {
			t.Fatalf("cluster rolebinding has not been stored: (%v)", clusterRoleBindingError)
		}

		testVerifyServiceAccountSpec(t, *foundServiceAccount, *foundClusterRole, *foundClusterRoleBinding)

	}

}

func TestEnsureService(t *testing.T) {
	ctx, keplerReconciler, keplerInstance, logger, client := generateDefaultOperatorSettings()

	numOfReconciliations := 3

	r := collectorReconciler{
		KeplerReconciler: *keplerReconciler,
		Instance:         keplerInstance,
		Ctx:              ctx,
	}

	for i := 0; i < numOfReconciliations; i++ {
		res, err := r.ensureService(logger)
		//basic check
		assert.True(t, res)
		if err != nil {
			t.Fatalf("service has failed which should not happen: (%v)", err)
		}
		foundService := &corev1.Service{}
		serviceError := client.Get(ctx, types.NamespacedName{Name: ServiceName, Namespace: ServiceNameSpace}, foundService)

		if serviceError != nil {
			t.Fatalf("service has not been stored: (%v)", serviceError)
		}

		testVerifyServiceSpec(t, *foundService)

	}
}

// Test CollectorReconciler As a Whole

func TestCollectorReconciler(t *testing.T) {
	ctx, keplerReconciler, keplerInstance, logger, client := generateDefaultOperatorSettings()
	numOfReconciliations := 3
	for i := 0; i < numOfReconciliations; i++ {
		_, err := CollectorReconciler(ctx, keplerInstance, keplerReconciler, logger)
		if err != nil {
			// This will never occur because such errors are handled already
			/*if strings.Contains(err.Error(), "no matches for kind") {
				if strings.Contains(err.Error(), "SecurityContextConstraints") || strings.Contains(err.Error(), "MachineConfig") {
					logger.V(1).Info("Not OpenShift skip SecurityContextConstraints and MachineConfig")
					continue
				}
			} else {*/
			t.Fatalf("collector reconciler has failed: (%v)", err)

		}
		//Run testVerifyCollectorReconciler
		testVerifyCollectorReconciler(t, ctx, client)

	}
}

func TestConfigMap(t *testing.T) {
	ctx, keplerReconciler, keplerInstance, logger, client := generateDefaultOperatorSettings()

	numOfReconciliations := 3

	r := collectorReconciler{
		KeplerReconciler: *keplerReconciler,
		Instance:         keplerInstance,
		Ctx:              ctx,
	}

	for i := 0; i < numOfReconciliations; i++ {
		res, err := r.ensureConfigMap(logger)
		//basic check
		assert.True(t, res)
		if err != nil {
			t.Fatalf("configmap has failed which should not happen: (%v)", err)
		}
		foundConfigMap := &corev1.ConfigMap{}
		configMapError := client.Get(ctx, types.NamespacedName{Name: CollectorConfigMapName, Namespace: CollectorConfigMapNameSpace}, foundConfigMap)

		if configMapError != nil {
			t.Fatalf("configmap has not been stored: (%v)", configMapError)
		}

		testVerifyConfigMap(t, *foundConfigMap)

	}

}

func TestSCC(t *testing.T) {
	ctx, keplerReconciler, keplerInstance, logger, client := generateDefaultOperatorSettings()

	numOfReconciliations := 3

	r := collectorReconciler{
		KeplerReconciler: *keplerReconciler,
		Instance:         keplerInstance,
		Ctx:              ctx,
	}

	for i := 0; i < numOfReconciliations; i++ {
		res, err := r.ensureSCC(logger)
		assert.True(t, res)
		if err != nil {
			t.Fatalf("scc has failed which should not happen: (%v)", err)
		}
		foundSCC := &securityv1.SecurityContextConstraints{}
		sccError := client.Get(ctx, types.NamespacedName{Name: SCCObjectName, Namespace: SCCObjectNameSpace}, foundSCC)

		if sccError != nil && strings.Contains(err.Error(), "no matches for kind") {
			fmt.Printf("resulting error not a timeout: %s", sccError)

		}
		if sccError != nil {
			t.Fatalf("scc has not been stored: (%v)", sccError)
		}
		testVerifySCC(t, *foundSCC)

	}
}

func TestBasicMachineConfig(t *testing.T) {
	ctx, keplerReconciler, keplerInstance, logger, client := generateDefaultOperatorSettings()

	numOfReconciliations := 3

	r := collectorReconciler{
		KeplerReconciler: *keplerReconciler,
		Instance:         keplerInstance,
		Ctx:              ctx,
	}

	for i := 0; i < numOfReconciliations; i++ {
		res, err := r.ensureMachineConfig(logger)
		assert.True(t, res)
		if err != nil {
			t.Fatalf("machineconfig has failed which should not happen: (%v)", err)
		}
		foundMasterCgroupKernelArgs := &mcfgv1.MachineConfig{}
		foundWorkerCgroupKernelArgs := &mcfgv1.MachineConfig{}
		foundMasterDevel := &mcfgv1.MachineConfig{}
		foundWorkerDevel := &mcfgv1.MachineConfig{}
		masterCgroupKernelArgsError := client.Get(ctx, types.NamespacedName{Name: MachineConfigCGroupKernelArgMasterName, Namespace: ""}, foundMasterCgroupKernelArgs)
		workerCgroupKernelArgsError := client.Get(ctx, types.NamespacedName{Name: MachineConfigCGroupKernelArgWorkerName, Namespace: ""}, foundWorkerCgroupKernelArgs)
		masterDevelError := client.Get(ctx, types.NamespacedName{Name: MachineConfigDevelMasterName, Namespace: ""}, foundMasterDevel)
		workerDevelError := client.Get(ctx, types.NamespacedName{Name: MachineConfigDevelWorkerName, Namespace: ""}, foundWorkerDevel)

		if masterCgroupKernelArgsError != nil && strings.Contains(masterCgroupKernelArgsError.Error(), "no matches for kind") {
			fmt.Printf("resulting error not a timeout: %s", masterCgroupKernelArgsError)
		} else {
			if masterCgroupKernelArgsError != nil {
				t.Fatalf("cgroup kernel arguments master machine config has not been stored: (%v)", masterCgroupKernelArgsError)
			}
			if workerCgroupKernelArgsError != nil {
				t.Fatalf("cgroup kernel arguments worker machine config has not been stored: (%v)", workerCgroupKernelArgsError)
			}

			if masterDevelError != nil {
				t.Fatalf("devel master machine config has not been stored: (%v)", masterDevelError)
			}

			if workerDevelError != nil {
				t.Fatalf("devel worker machine config has not been stored: (%v)", workerDevelError)
			}

			testVerifyBasicMachineConfig(t, *foundMasterCgroupKernelArgs, *foundWorkerCgroupKernelArgs, *foundMasterDevel, *foundWorkerDevel)
		}
	}

}