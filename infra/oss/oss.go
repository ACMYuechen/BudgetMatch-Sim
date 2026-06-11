package oss

import (
	"fmt"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type Config struct {
	Endpoint        string `json:"endpoint"`
	AccessKeyID     string `json:"accessKeyID"`
	AccessKeySecret string `json:"accessKeySecret"`
	BucketName      string `json:"bucketName"`
	Domain          string `json:"domain"`
}

type OSS struct {
	Client *oss.Client
	Bucket *oss.Bucket
}

func NewOSS(cfg Config) (*OSS, error) {
	client, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSS client: %w", err)
	}

	bucket, err := client.Bucket(cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	return &OSS{
		Client: client,
		Bucket: bucket,
	}, nil
}
