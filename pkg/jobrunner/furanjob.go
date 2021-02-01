package jobrunner

import (
	"encoding/json"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dollarshaveclub/furan/pkg/models"
)

var (
	jobParallelism           = int32(1)
	jobCompletions           = int32(1)
	jobBackoffLimit          = int32(3)
	jobActiveDeadlineSeconds = int64(60 * 60) // 1 hour
	shareProcessNamespace    = true
)

const (
	bkSocketMountPath = "/run/buildkit"
	bkSocketName      = "buildkitd.sock"
)

// furanjob returns a Job to execute a single image build/push
// It utilizes BuildKit listening on a UNIX socket, shared with the Furan container by an emptyDir volume
// The Furan container (sidecar, in a sense) executes the build/push via the BuildKit gRPC API, records progress in the
// database and exits cleanly when finished
func furanjob() batchv1.Job {
	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "",
			Namespace:   "",
			Labels:      nil,
			Annotations: nil,
		},
		Spec: batchv1.JobSpec{
			Parallelism:           &jobParallelism,
			Completions:           &jobCompletions,
			ActiveDeadlineSeconds: &jobActiveDeadlineSeconds,
			BackoffLimit:          &jobBackoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					// Metadata is set below
					Name: "",
					Annotations: map[string]string{
						"container.apparmor.security.beta.kubernetes.io/buildkitd": "unconfined",
						"container.seccomp.security.alpha.kubernetes.io/buildkitd": "unconfined",
					},
				},
				Spec: corev1.PodSpec{
					ShareProcessNamespace: &shareProcessNamespace,
					RestartPolicy:         corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						corev1.Container{
							Name:            "furan",
							Image:           "", // injected below with the image tag set on the server pod creating the job
							ImagePullPolicy: "IfNotPresent",
							Command: []string{
								"/usr/local/bin/furan",
							},
							Args: []string{
								// all root flags are injected here (secrets setup, etc)
								"runbuild",
								"--buildkit-addr",
								"unix://" + bkSocketMountPath + "/" + bkSocketName,
								// "--build-id <id>" is injected here
							},
							VolumeMounts: []corev1.VolumeMount{
								corev1.VolumeMount{
									Name:      "bksocket",
									MountPath: bkSocketMountPath,
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits:   corev1.ResourceList{},
								Requests: corev1.ResourceList{},
							},
						},
						corev1.Container{
							Name:            "buildkitd",
							Image:           BuildKitImage,
							ImagePullPolicy: "IfNotPresent",
							Args: []string{
								"--oci-worker-no-process-sandbox",
								"--addr",
								"unix://" + bkSocketMountPath + "/" + bkSocketName,
							},
							VolumeMounts: []corev1.VolumeMount{
								corev1.VolumeMount{
									Name:      "bksocket",
									MountPath: bkSocketMountPath,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("512M"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2G"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						corev1.Volume{
							Name: "bksocket",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium: corev1.StorageMediumMemory,
								},
							},
						},
					},
					ImagePullSecrets: nil,
				},
			},
		},
	}
}

// truncateName returns the first n runes of s if the length exceeds N
func truncateName(s string, n uint) string {
	if r := []rune(s); len(r) > int(n) {
		return string(r[0 : n-1])
	}
	return s
}

// JobLabel is a label added to every build job to aid search/aggregation
var JobLabel = "created-by:furan2"

var BuildKitImage = "932427637498.dkr.ecr.us-west-2.amazonaws.com/furan2-builder:v0.7.2-rootless"

// FuranJobFunc is a JobFactoryFunc that generates a Kubernetes Job to execute a build
func FuranJobFunc(info ImageInfo, build models.Build, bkresources [2]corev1.ResourceList) *batchv1.Job {
	j := furanjob()
	j.Namespace = info.Namespace
	j.Name = truncateName("furan-build-"+strings.Replace(build.GitHubRepo, "/", "-", -1)+"-"+build.ID.String(), 63)
	jlabel := strings.Split(JobLabel, ":")
	if len(jlabel) != 2 {
		panic(fmt.Errorf("invalid job label (<name>:<value> is required): %v", JobLabel))
	}
	j.Labels = map[string]string{
		// a valid label must be an empty string or consist of alphanumeric characters,
		// '-', '_' or '.', and must start and end with an alphanumeric character (e.g.
		//'MyValue',  or 'my_value',  or '12345', regex used for validation is
		//'(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?')
		"build-id": build.ID.String(),
		jlabel[0]:  jlabel[1],
	}
	reqj, _ := json.Marshal(build.Request) // ignore error
	j.Annotations = map[string]string{
		"build-request": string(reqj), // if reqj == nil, this will be an empty string
		"source-repo":   build.GitHubRepo,
		"source-ref":    build.GitHubRef,
		"dest-repo":     fmt.Sprintf("%v", build.ImageRepos),
		"image-tags":    strings.Join(build.Tags, ","),
	}
	j.Spec.Template.Spec.ServiceAccountName = info.ServiceAccount
	j.Spec.Template.Spec.Containers[0].Image = info.Image
	j.Spec.Template.Spec.Containers[0].Args = append(
		j.Spec.Template.Spec.Containers[0].Args,
		"--build-id",
		build.ID.String())
	j.Spec.Template.Spec.Containers[0].Args = append(
		info.RootArgs,
		j.Spec.Template.Spec.Containers[0].Args...,
	)
	for i := range info.EnvVars {
		env := info.EnvVars[i]
		j.Spec.Template.Spec.Containers[0].Env = append(j.Spec.Template.Spec.Containers[0].Env, env)
	}
	for _, ips := range info.ImagePullSecrets {
		j.Spec.Template.Spec.ImagePullSecrets = append(
			j.Spec.Template.Spec.ImagePullSecrets,
			corev1.LocalObjectReference{Name: ips})
	}
	j.Spec.Template.Spec.Containers[0].Resources.Requests = info.Resources[0]
	j.Spec.Template.Spec.Containers[0].Resources.Limits = info.Resources[1]
	if bkresources[0] != nil {
		j.Spec.Template.Spec.Containers[1].Resources.Requests = bkresources[0]
	}
	if bkresources[1] != nil {
		j.Spec.Template.Spec.Containers[1].Resources.Limits = bkresources[1]
	}
	return &j
}

var _ JobFactoryFunc = FuranJobFunc
