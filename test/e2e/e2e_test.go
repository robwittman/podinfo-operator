/*
Copyright 2022.

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
	"strings"
	"time"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/ginkgo/v2"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/gomega"
)

// constant parts of the file
const namespace = "podinfo-operator-system"

// NOTE: The resources were deploying here are very similar to the
// memcached-operator example provided by the operator-sdk. Most of this was borrowed
// from those samples, with modifications and substitutions as necessary
// https://github.com/operator-framework/operator-sdk/blob/master/testdata/go/v3/memcached-operator/test/e2e/e2e_test.go
var _ = Describe("podinfo", Ordered, func() {
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, _ = Run(cmd)
	})
	AfterAll(func() {
		By("remove CRD")
		cmd := exec.Command("kubectl", "delete", "-f", "config/samples/apps_v1alpha1_podinfo.yaml", "-n", namespace)
		_, _ = Run(cmd)
		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = Run(cmd)
	})

	Context("PodInfo Operator", func() {
		It("should run successfully", func() {
			var controllerPodName string
			var err error

			// operatorImage stores the name of the image used in the example
			var operatorImage = "apps.podinfo.io/v1alpha1:v0.0.1"

			By("building the manager(Operator) image")
			cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", operatorImage))
			_, err = Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("loading the the manager(Operator) image on Kind")
			err = LoadImageToKindClusterWithName(operatorImage)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("installing CRDs")
			cmd = exec.Command("make", "install")
			_, err = Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("setting up RBAC")
			cmd = exec.Command("bash", "-c", "kustomize build test | kubectl apply -f - -n "+namespace)
			_, err = Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("deploying the controller-manager")
			cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", operatorImage))
			_, err = Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func() error {
				// Get pod name
				cmd = exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}{{ if not .metadata.deletionTimestamp }}{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)
				podOutput, err := Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				podNames := GetNonEmptyLines(string(podOutput))
				if len(podNames) != 1 {
					return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
				}
				controllerPodName = podNames[0]
				ExpectWithOffset(2, controllerPodName).Should(ContainSubstring("controller-manager"))

				// Validate pod status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if string(status) != "Running" {
					return fmt.Errorf("controller pod in %s status", status)
				}
				return nil
			}
			EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).Should(Succeed())

			By("creating an instance of the PodInfo CRD")
			EventuallyWithOffset(1, func() error {
				cmd = exec.Command("kubectl", "apply", "-f", "config/samples/apps_v1alpha1_podinfo.yaml", "-n", namespace)
				_, err = Run(cmd)
				return err
			}, time.Minute, time.Second).Should(Succeed())

			By("validating that pod(s) status.phase=Running")
			getPodInfoStatus := func() error {
				cmd = exec.Command("kubectl", "get",
					"pods", "-l", "app=podinfo",
					"-o", "jsonpath={.items[*].status}", "-n", namespace,
				)
				status, err := Run(cmd)
				fmt.Println(string(status))
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if !strings.Contains(string(status), "\"phase\":\"Running\"") {
					return fmt.Errorf("podinfo pod in %s status", status)
				}
				return nil
			}
			EventuallyWithOffset(1, getPodInfoStatus, time.Minute, time.Second).Should(Succeed())

			By("validating that the status of the custom resource created is updated or not")
			getStatus := func() error {
				cmd = exec.Command("kubectl", "get", "podinfo",
					"podinfo-sample", "-o", "jsonpath={.status.conditions}",
					"-n", namespace,
				)
				status, err := Run(cmd)
				fmt.Println(string(status))
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if !strings.Contains(string(status), "Available") {
					return fmt.Errorf("status condition with type Available should be set")
				}
				return nil
			}
			Eventually(getStatus, time.Minute, time.Second).Should(Succeed())
		})
	})
})
