/*
Copyright 2018 The Kubernetes Authors.

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

package settings

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/assert"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/ingress-nginx/test/e2e/framework"
)

var _ = framework.IngressNginxDescribe("[Security] Pod Security Policies with volumes", func() {
	f := framework.NewDefaultFramework("pod-security-policies-volumes")

	ginkgo.It("should be running with a Pod Security Policy", func() {

		k8sversion, err := f.KubeClientSet.Discovery().ServerVersion()
		if err != nil {
			assert.Nil(ginkgo.GinkgoT(), err, "getting version")
		}

		numversion, err := strconv.Atoi(k8sversion.Minor)
		if err != nil {
			assert.Nil(ginkgo.GinkgoT(), err, "converting version")
		}

		if numversion > 24 {
			ginkgo.Skip("PSP not supported in this version")
		}
		psp := createPodSecurityPolicy()
		_, err = f.KubeClientSet.PolicyV1beta1().PodSecurityPolicies().Create(context.TODO(), psp, metav1.CreateOptions{})
		if !k8sErrors.IsAlreadyExists(err) {
			assert.Nil(ginkgo.GinkgoT(), err, "creating Pod Security Policy")
		}

		role, err := f.KubeClientSet.RbacV1().Roles(f.Namespace).Get(context.TODO(), "nginx-ingress", metav1.GetOptions{})
		assert.Nil(ginkgo.GinkgoT(), err, "getting ingress controller cluster role")
		assert.NotNil(ginkgo.GinkgoT(), role)

		role.Rules = append(role.Rules, rbacv1.PolicyRule{
			APIGroups:     []string{"policy"},
			Resources:     []string{"podsecuritypolicies"},
			ResourceNames: []string{ingressControllerPSP},
			Verbs:         []string{"use"},
		})

		_, err = f.KubeClientSet.RbacV1().Roles(f.Namespace).Update(context.TODO(), role, metav1.UpdateOptions{})
		assert.Nil(ginkgo.GinkgoT(), err, "updating ingress controller cluster role to use a pod security policy")

		err = f.UpdateIngressControllerDeployment(func(deployment *appsv1.Deployment) error {
			args := deployment.Spec.Template.Spec.Containers[0].Args
			args = append(args, "--v=2")
			deployment.Spec.Template.Spec.Containers[0].Args = args

			volumes := deployment.Spec.Template.Spec.Volumes
			volumes = append(
			        volumes,
			        corev1.Volume{
                        Name: "ssl", VolumeSource: corev1.VolumeSource{
                            EmptyDir: &corev1.EmptyDirVolumeSource{},
                        },
                    },
                    corev1.Volume{
                        Name: "tmp", VolumeSource: corev1.VolumeSource{
                            EmptyDir: &corev1.EmptyDirVolumeSource{},
                        },
                    },
            )
			deployment.Spec.Template.Spec.Volumes = volumes

			fsGroup := int64(33)
			deployment.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
				FSGroup: &fsGroup,
			}

            volumeMounts := deployment.Spec.Template.Spec.Containers[0].VolumeMounts
            volumeMounts = append(
                    volumeMounts,
                    corev1.VolumeMount{
                        Name: "ssl", MountPath: "/etc/my-amazing-ssl",
                    },
                    corev1.VolumeMount{
                        Name: "tmp", MountPath: "/my-other-tmp",
                    },
            )
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts = volumeMounts

			_, err := f.KubeClientSet.AppsV1().Deployments(f.Namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{})

			return err
		})
		assert.Nil(ginkgo.GinkgoT(), err, "updating ingress controller deployment")

		f.WaitForNginxListening(80)

		f.NewEchoDeployment()

		f.WaitForNginxConfiguration(
			func(cfg string) bool {
				return strings.Contains(cfg, "server_tokens off")
			})

		f.HTTPTestClient().
			GET("/").
			WithHeader("Host", "foo.bar.com").
			Expect().
			Status(http.StatusNotFound)
	})
})
