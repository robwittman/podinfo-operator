package main

import (
	"context"
	"fmt"
	helmclient "github.com/mittwald/go-helm-client"
)

func test() {
	helmClient, err := helmclient.New(&helmclient.Options{
		Namespace: "default", // Change this to the namespace you wish the client to operate in.
	})
	if err != nil {
		panic(err)
	}
	//chartRepo := repo.Entry{
	//	Name: "bitnami",
	//	URL:  "oci://registry-1.docker.io/bitnamicharts",
	//	// Since helm 3.6.1 it is necessary to pass 'PassCredentialsAll = true'.
	//	PassCredentialsAll: true,
	//}
	//
	//if err := helmClient.AddOrUpdateChartRepo(chartRepo); err != nil {
	//	panic(err)
	//}
	//
	//if err := helmClient.UpdateChartRepos(); err != nil {
	//	log.Fatal(err)
	//}

	rel, err := helmClient.InstallChart(context.TODO(), &helmclient.ChartSpec{
		ReleaseName: "podinfo-redis",
		//ChartName:   "oci://registry-1.docker.io/bitnamicharts/redis",
		Namespace: "default",
	}, nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(rel)
	fmt.Println("Completed")
}
