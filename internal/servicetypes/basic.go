package servicetypes

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// defines all the basic service type defaults
var basic = ServiceType{
	Name: "basic",
	Ports: ServicePorts{
		CanChangePort: true,
		Ports: []corev1.ServicePort{
			{
				Port: 3000,
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "http",
				},
				Protocol: corev1.ProtocolTCP,
				Name:     "http",
			},
		},
	},
	PrimaryContainer: ServiceContainer{
		Name:            "basic",
		ImagePullPolicy: corev1.PullAlways,
		Container: corev1.Container{
			Ports: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 3000,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.IntOrString{
							Type:   intstr.Int,
							IntVal: 3000,
						},
					},
				},
				InitialDelaySeconds: 1,
				TimeoutSeconds:      1,
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.IntOrString{
							Type:   intstr.Int,
							IntVal: 3000,
						},
					},
				},
				InitialDelaySeconds: 60,
				TimeoutSeconds:      10,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("100M"),
				},
			},
		},
	},
}

// contains all the persistent type overrides that the basic service doesn't have
var basicPersistent = ServiceType{
	Name:  "basic-persistent",
	Ports: basic.Ports,
	PrimaryContainer: ServiceContainer{
		Name:            basic.PrimaryContainer.Name,
		ImagePullPolicy: basic.PrimaryContainer.ImagePullPolicy,
		Container:       basic.PrimaryContainer.Container,
	},
	Volumes: ServiceVolume{
		PersistentVolumeSize: "5Gi",
		PersistentVolumeType: corev1.ReadWriteMany,
		Backup:               true,
	},
}
