package specialresource

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func setOSforBuild(obj *unstructured.Unstructured, rel string, ver string) {

	log.Info("OS", "rel", rel)
	log.Info("OS", "ver", ver)

	var nodeOS string

	if strings.Compare(rel, "rhcos") == 0 && strings.Compare(ver, "4") == 0 {
		log.Info("Setting OS to rhel8")
		nodeOS = "rhel8"
	}

	if strings.Compare(rel, "rhel") == 0 && strings.Compare(ver, "8") == 0 {
		log.Info("Setting OS to rhel8")
		nodeOS = "rhel8"
	}

	if strings.Compare(rel, "rhel") == 0 && strings.Compare(ver, "7") == 0 {
		log.Info("Setting OS to rhel7")
		nodeOS = "rhel7"
	}

	err := unstructured.SetNestedField(obj.Object, nodeOS, "spec", "source", "git", "ref")
	checkNestedFields(true, err)

	err = unstructured.SetNestedField(obj.Object, nodeOS, "spec", "source", "contextDir")
	checkNestedFields(true, err)
}

func prefixNVIDIABuildConfig(obj *unstructured.Unstructured, r *ReconcileSpecialResource) error {

	var nodeOSrel string
	var nodeOSver string

	for _, node := range node.list.Items {
		labels := node.GetLabels()
		nodeOSrel = labels["feature.node.kubernetes.io/system-os_release.ID"]
		nodeOSver = labels["feature.node.kubernetes.io/system-os_release.VERSION_ID.major"]

		if len(nodeOSrel) == 0 || len(nodeOSver) == 0 {
			return fmt.Errorf("Cannot extract feature.node.kubernetes.io/system-os_release.*, is NFD running? Check node labels")
		}
		continue
	}

	setOSforBuild(obj, nodeOSrel, nodeOSver)

	return nil
}

func prefixNVIDIAdriverDaemonset(obj *unstructured.Unstructured, r *ReconcileSpecialResource) error {

	containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	checkNestedFields(found, err)

	kernelVersion := kernelFullVersion(r)

	if kernelVersion == "" {
		return fmt.Errorf("Cannot extract kernelVersion from a Special Resource Node, scale up a Special Resource Node")
	}

	for _, container := range containers {
		switch container := container.(type) {
		case map[string]interface{}:
			if container["name"] == "nvidia-driver-ctr" {
				img, found, err := unstructured.NestedString(container, "image")
				checkNestedFields(found, err)
				img = strings.Replace(img, "KERNEL_FULL_VERSION", kernelVersion, -1)
				err = unstructured.SetNestedField(container, img, "image")
				checkNestedFields(true, err)
			}
		default:
			panic(fmt.Errorf("cannot extract name,image from %T", container))
		}
	}

	err = unstructured.SetNestedSlice(obj.Object, containers,
		"spec", "template", "spec", "containers")
	checkNestedFields(true, err)

	err = unstructured.SetNestedField(obj.Object, kernelVersion,
		"spec", "template", "spec", "nodeSelector", "feature.node.kubernetes.io/kernel-version.full")
	checkNestedFields(true, err)

	return nil
}

func kernelFullVersion(r *ReconcileSpecialResource) string {

	logger := log.WithValues("Request.Namespace", "default", "Request.Name", "Node")
	// Assuming all nodes are running the same kernel version,
	// One could easily add driver-kernel-versions for each node.
	for _, node := range node.list.Items {
		labels := node.GetLabels()

		var ok bool
		kernelFullVersion, ok := labels["feature.node.kubernetes.io/kernel-version.full"]
		if ok {
			logger.Info(kernelFullVersion)
		} else {
			err := errors.NewNotFound(schema.GroupResource{Group: "Node", Resource: "Label"},
				"feature.node.kubernetes.io/kernel-version.full")
			logger.Info("Couldn't get kernelVersion", err)
			return ""
		}
		return kernelFullVersion
	}
	return ""
}

func getPromURLPass(obj *unstructured.Unstructured, r *ReconcileSpecialResource) (string, string, error) {

	promURL := ""
	promPass := ""

	grafSecret, err := kubeclient.CoreV1().Secrets("openshift-monitoring").Get("grafana-datasources", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "")
		return promURL, promPass, err
	}

	promJSON := grafSecret.Data["prometheus.yaml"]

	sec := &unstructured.Unstructured{}

	if err := json.Unmarshal(promJSON, &sec.Object); err != nil {
		log.Error(err, "UnmarshlJSON")
		return promURL, promPass, err
	}

	datasources, found, err := unstructured.NestedSlice(sec.Object, "datasources")
	checkNestedFields(found, err)

	for _, datasource := range datasources {
		switch datasource := datasource.(type) {
		case map[string]interface{}:
			promURL, found, err = unstructured.NestedString(datasource, "url")
			checkNestedFields(found, err)
			promPass, found, err = unstructured.NestedString(datasource, "basicAuthPassword")
			checkNestedFields(found, err)
		default:
			log.Info("PROM", "DEFAULT NOT THE CORRECT TYPE", promURL)
		}
		break
	}

	return promURL, promPass, nil
}

func prefixNVIDIAgrafanaConfigMap(obj *unstructured.Unstructured, r *ReconcileSpecialResource) error {

	promData, found, err := unstructured.NestedString(obj.Object, "data", "ocp-prometheus.yml")
	checkNestedFields(found, err)

	promURL, promPass, err := getPromURLPass(obj, r)
	if err != nil {
		return err
	}

	promData = strings.Replace(promData, "REPLACE_PROM_URL", promURL, -1)
	promData = strings.Replace(promData, "REPLACE_PROM_PASS", promPass, -1)
	promData = strings.Replace(promData, "REPLACE_PROM_USER", "internal", -1)

	//log.Info("PROM", "DATA", promData)
	if err := unstructured.SetNestedField(obj.Object, promData, "data", "ocp-prometheus.yml"); err != nil {
		log.Error(err, "Couldn't update ocp-prometheus.yml")
		return err
	}

	return nil
}
