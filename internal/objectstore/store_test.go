package objectstore

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestObjectKey(t *testing.T) {
	s := &Store{cfg: Config{Prefix: "debridnest"}}

	tests := []struct {
		infoHash string
		filePath string
		want     string
	}{
		{
			infoHash: "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			filePath: "/Movie.mkv",
			want:     "debridnest/abcdef0123456789abcdef0123456789abcdef01/Movie.mkv",
		},
		{
			infoHash: "abc",
			filePath: "folder/file.mp4",
			want:     "debridnest/abc/folder/file.mp4",
		},
	}

	for _, tc := range tests {
		got := s.ObjectKey(tc.infoHash, tc.filePath)
		if got != tc.want {
			t.Fatalf("ObjectKey(%q, %q) = %q, want %q", tc.infoHash, tc.filePath, got, tc.want)
		}
	}
}

func TestObjectKeyNoPrefix(t *testing.T) {
	s := &Store{cfg: Config{}}
	got := s.ObjectKey("HASH", "/a/b.mkv")
	want := "hash/a/b.mkv"
	if got != want {
		t.Fatalf("ObjectKey without prefix = %q, want %q", got, want)
	}
}

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "")

	cfg := LoadFromEnv()
	if cfg.Enabled {
		t.Fatal("expected S3 disabled by default")
	}
	if cfg.Region != "auto" {
		t.Fatalf("default region = %q, want auto", cfg.Region)
	}
	if cfg.OffloadLocal {
		t.Fatal("expected OffloadLocal false by default")
	}
}

func TestNewDisabled(t *testing.T) {
	store, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatalf("New disabled: %v", err)
	}
	if store.Enabled() {
		t.Fatal("expected store disabled")
	}
}

func TestNewEnabledRequiresBucket(t *testing.T) {
	_, err := New(Config{Enabled: true})
	if err == nil {
		t.Fatal("expected error when bucket missing")
	}
}

func TestEnabled(t *testing.T) {
	store := &Store{cfg: Config{Enabled: true}}
	if store.Enabled() {
		t.Fatal("Enabled() should be false without client")
	}
}

func TestObjectKeySanitizesPaths(t *testing.T) {
	s := &Store{cfg: Config{Prefix: "nest"}}

	tests := []struct {
		filePath string
		want     string
	}{
		{filePath: `\Season 1\ep.mkv`, want: "nest/abc/Season 1/ep.mkv"},
		{filePath: "///nested//file.mkv", want: "nest/abc/nested/file.mkv"},
		{filePath: "..\\..\\etc/passwd", want: "nest/abc/../../etc/passwd"},
	}

	for _, tc := range tests {
		got := s.ObjectKey("ABC", tc.filePath)
		if got != tc.want {
			t.Fatalf("ObjectKey(%q) = %q, want %q", tc.filePath, got, tc.want)
		}
	}
}

func TestLoadFromEnvEnabled(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "1")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "my-bucket")
	t.Setenv("DEBRIDNEST_S3_ENDPOINT", "https://s3.example.com")
	t.Setenv("DEBRIDNEST_S3_FORCE_PATH_STYLE", "1")
	t.Setenv("DEBRIDNEST_S3_OFFLOAD_LOCAL", "1")
	t.Setenv("DEBRIDNEST_S3_QUOTA_GB", "500")

	cfg := LoadFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Bucket != "my-bucket" {
		t.Fatalf("bucket = %q", cfg.Bucket)
	}
	if !cfg.ForcePathStyle || !cfg.OffloadLocal {
		t.Fatalf("flags: pathStyle=%v offload=%v", cfg.ForcePathStyle, cfg.OffloadLocal)
	}
	if cfg.QuotaGB != 500 {
		t.Fatalf("quota = %d, want 500", cfg.QuotaGB)
	}
}

func TestLoadFromEnvDirectMode(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "1")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "my-bucket")
	t.Setenv("DEBRIDNEST_S3_DIRECT", "1")
	t.Setenv("DEBRIDNEST_S3_OFFLOAD_LOCAL", "")
	t.Setenv("DEBRIDNEST_S3_EARLY_OFFLOAD", "")

	cfg := LoadFromEnv()
	if !cfg.EarlyOffload {
		t.Fatal("expected EarlyOffload true when S3_DIRECT=1")
	}
	if !cfg.OffloadLocal {
		t.Fatal("expected OffloadLocal true by default when S3_DIRECT=1")
	}
}

func TestLoadFromEnvDirectRespectsExplicitOffloadLocalOff(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "1")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "my-bucket")
	t.Setenv("DEBRIDNEST_S3_DIRECT", "1")
	t.Setenv("DEBRIDNEST_S3_OFFLOAD_LOCAL", "0")

	cfg := LoadFromEnv()
	if !cfg.EarlyOffload {
		t.Fatal("expected EarlyOffload true when S3_DIRECT=1")
	}
	if cfg.OffloadLocal {
		t.Fatal("expected OffloadLocal false when explicitly disabled")
	}
}

func TestListObjectsUsesDelimiter(t *testing.T) {
	modified := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		listOutputs: []*s3.ListObjectsV2Output{
			{
				CommonPrefixes: []types.CommonPrefix{{Prefix: aws.String("nest/hash/")}},
				Contents: []types.Object{
					{Key: aws.String("nest/root.mkv"), Size: aws.Int64(123), LastModified: &modified},
				},
			},
		},
	}
	store := &Store{cfg: Config{Enabled: true, Bucket: "bucket"}, client: client}

	result, err := store.List(context.Background(), "/nest//", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if client.listInputs[0].Prefix == nil || *client.listInputs[0].Prefix != "nest/" {
		t.Fatalf("prefix = %v, want nest/", client.listInputs[0].Prefix)
	}
	if client.listInputs[0].Delimiter == nil || *client.listInputs[0].Delimiter != "/" {
		t.Fatalf("delimiter = %v, want /", client.listInputs[0].Delimiter)
	}
	if len(result.Objects) != 2 {
		t.Fatalf("objects = %d, want 2", len(result.Objects))
	}
	if !result.Objects[0].Directory || result.Objects[0].Key != "nest/hash/" {
		t.Fatalf("directory object = %+v", result.Objects[0])
	}
	if result.Objects[1].Directory || result.Objects[1].Key != "nest/root.mkv" || result.Objects[1].Size != 123 {
		t.Fatalf("file object = %+v", result.Objects[1])
	}
}

func TestListAllObjectsPaginates(t *testing.T) {
	client := &fakeS3Client{
		listOutputs: []*s3.ListObjectsV2Output{
			{
				Contents:              []types.Object{{Key: aws.String("nest/a.mkv"), Size: aws.Int64(1)}},
				IsTruncated:           aws.Bool(true),
				NextContinuationToken: aws.String("next"),
			},
			{
				Contents: []types.Object{{Key: aws.String("nest/b.mkv"), Size: aws.Int64(2)}},
			},
		},
	}
	store := &Store{cfg: Config{Enabled: true, Bucket: "bucket"}, client: client}

	result, err := store.ListAll(context.Background(), "nest/", 100)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(result.Objects) != 2 {
		t.Fatalf("objects = %d, want 2", len(result.Objects))
	}
	if client.listInputs[1].ContinuationToken == nil || *client.listInputs[1].ContinuationToken != "next" {
		t.Fatalf("continuation token = %v, want next", client.listInputs[1].ContinuationToken)
	}
}

type fakeS3Client struct {
	listOutputs []*s3.ListObjectsV2Output
	listInputs  []*s3.ListObjectsV2Input
}

func (f *fakeS3Client) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}

func (f *fakeS3Client) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	f.listInputs = append(f.listInputs, in)
	if len(f.listOutputs) == 0 {
		return &s3.ListObjectsV2Output{}, nil
	}
	out := f.listOutputs[0]
	f.listOutputs = f.listOutputs[1:]
	return out, nil
}

func (f *fakeS3Client) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3Client) DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3Client) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return &s3.HeadObjectOutput{}, nil
}

func (f *fakeS3Client) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(""))}, nil
}
