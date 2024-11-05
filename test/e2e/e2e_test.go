/*
Copyright 2024.

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
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dovics/extendeddeployment/test/utils"
)

// namespace where the project is deployed in
const (
	namespace     = "extended-system"
	testNamespace = "extended-test"
)

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// installing CRDs, and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]

				g.Expect(controllerPodName).To(ContainSubstring("extended-controller-manager"))
				utils.ExpectPodRunning(g, controllerPodName, namespace)
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		// TODO: Customize the e2e test suite with scenarios specific to your project.
		// Consider applying sample/CR(s) and check their status and/or verifying
		// the reconciliation by using the metrics, i.e.:
		// metricsOutput := getMetricsOutput()
		// Expect(metricsOutput).To(ContainSubstring(
		//    fmt.Sprintf(`controller_runtime_reconcile_total{controller="%s",result="success"} 1`,
		//    strings.ToLower(<Kind>),
		// ))
	})

	Context("ExtendedDeployment", func() {
		BeforeAll(func() {
			By("creating test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")
		})

		AfterAll(func() {
			By("removing test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNamespace)
			_, _ = utils.Run(cmd)
		})

		BeforeEach(func() {
			By("deploying the sample")
			cmd := exec.Command("make", "deploy-sample", fmt.Sprintf("NAMESPACE=%s", testNamespace))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to deploy the sample")
		})

		AfterEach(func() {
			By("undeploying the sample")
			cmd := exec.Command("make", "undeploy-sample", fmt.Sprintf("NAMESPACE=%s", testNamespace))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to undeploy the controller-manager")
		})

		It("sample run successfully", func() {
			By("validating that the extendeddeployment pod is running as expected")
			verifySampleUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "app=test",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", testNamespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve sample pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(2), "expected 2 pod running")
				for _, podName := range podNames {
					g.Expect(podName).To(ContainSubstring("extendeddeployment-sample"),
						"expected pod name to contain extendeddeployment-sample")
					utils.ExpectPodRunning(g, podName, testNamespace)
				}
			}
			Eventually(verifySampleUp).Should(Succeed())
		})

		It("sample inplace update should work", func() {
			By("checking the sample deployment")
			inplaceUpdateSuccess := func(g Gomega) {

				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "app=test",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", testNamespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve sample pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(2), "expected 2 pod running")
				for _, podName := range podNames {
					utils.ExpectPodRunning(g, podName, testNamespace)
				}

				cmd = exec.Command("kubectl", "patch", "extendeddeployment",
					"extendeddeployment-sample", "--type", "json",
					"-p", "[{\"op\": \"replace\", "+
						"\"path\": \"/spec/template/spec/containers/0/image\", \"value\":\"busybox\"}]",
					"-n", testNamespace,
				)
				_, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to patch extended deployment")

				_, _ = utils.Run(exec.Command("bash", "-c", "sleep 30"))
				cmd = exec.Command("kubectl", "get",
					"pods", "-l", "app=test",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", testNamespace,
				)

				podOutput, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNamesAfterUpdate := utils.GetNonEmptyLines(podOutput)

				g.Expect(podNamesAfterUpdate).Should(ContainElements(podNames))
				for _, podName := range podNamesAfterUpdate {
					utils.ExpectPodRunning(g, podName, testNamespace)
				}
			}
			Eventually(inplaceUpdateSuccess).Should(Succeed())
		})
	})

})
