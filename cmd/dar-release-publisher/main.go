package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/SalehElnagar/dar-download-app/internal/distribution"
	"github.com/SalehElnagar/dar-download-app/internal/publication"
)

const storageAudience = "https://storage.azure.com/"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	runtime := commandRuntime{
		environment: environmentMap(os.Environ()), stdout: os.Stdout, stderr: os.Stderr,
	}
	os.Exit(runtime.execute(ctx, os.Args[1:]))
}

func run(args []string, environment map[string]string, stdout, stderr io.Writer) int {
	runtime := commandRuntime{environment: environment, stdout: stdout, stderr: stderr}
	return runtime.execute(context.Background(), args)
}

type commandRuntime struct {
	environment map[string]string
	stdout      io.Writer
	stderr      io.Writer
}

func (runtime commandRuntime) execute(ctx context.Context, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(runtime.stderr, "expected validate or publish")
		return 2
	}
	switch args[0] {
	case "validate":
		if err := validateCommand(args[1:], runtime.stdout); err != nil {
			_, _ = fmt.Fprintln(runtime.stderr, "release validation failed")
			return 1
		}
		return 0
	case "publish":
		if len(args) != 1 || publishCommand(ctx, runtime.environment, runtime.stdout) != nil {
			_, _ = fmt.Fprintln(runtime.stderr, "release publication failed")
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintln(runtime.stderr, "unknown command")
		return 2
	}
}

func validateCommand(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repositoryRoot := flags.String("repository-root", "", "repository root")
	releaseID := flags.String("release-id", "", "release identifier")
	recipientsFile := flags.String("recipients-file", "", "protected recipients file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return publication.ErrSource
	}
	source, recipients, err := loadInput(*repositoryRoot, *releaseID, *recipientsFile)
	if err != nil {
		return err
	}
	return writeJSON(output, struct {
		ArtifactSHA256 string `json:"artifact_sha256"`
		RecipientCount int    `json:"recipient_count"`
		ReleaseID      string `json:"release_id"`
		ReleaseVersion string `json:"release_version"`
		Status         string `json:"status"`
	}{
		ArtifactSHA256: source.DARSHA256, RecipientCount: len(recipients),
		ReleaseID: source.ReleaseID, ReleaseVersion: source.Version, Status: "VALID",
	})
}

func publishCommand(ctx context.Context, values map[string]string, output io.Writer) error {
	environment, err := publication.ParseEnvironment(values)
	if err != nil {
		return err
	}
	source, recipients, err := loadInput(
		environment.RepositoryRoot, environment.ReleaseID, environment.RecipientsFile,
	)
	if err != nil {
		return err
	}
	credential, err := azidentity.NewAzureCLICredential(nil)
	if err != nil {
		return publication.ErrPublication
	}
	storageClient, err := service.NewClient(
		"https://"+environment.StorageAccount+".blob.core.windows.net/",
		credential,
		&service.ClientOptions{Audience: storageAudience},
	)
	if err != nil {
		return publication.ErrPublication
	}
	blobs, err := publication.NewAzureBlobWriter(storageClient)
	if err != nil {
		return err
	}
	serviceBus, err := azservicebus.NewClient(environment.ServiceBusNamespace, credential, nil)
	if err != nil {
		return publication.ErrPublication
	}
	defer serviceBus.Close(context.Background())
	sender, err := serviceBus.NewSender(environment.ServiceBusQueue, nil)
	if err != nil {
		return publication.ErrPublication
	}
	defer sender.Close(context.Background())
	queue, err := publication.NewAzureQueueWriter(sender)
	if err != nil {
		return err
	}
	publisher, err := publication.NewPublisher(environment.PublisherConfig(), blobs, queue)
	if err != nil {
		return err
	}
	result, err := publisher.Publish(ctx, publication.PublishRequest{
		RepositoryRoot: environment.RepositoryRoot, Source: source, Recipients: recipients,
		CommitSHA: environment.CommitSHA, CreatedAt: environment.CreatedAt,
	})
	if err != nil {
		return err
	}
	return writeJSON(output, struct {
		publication.PublicationResult
		Status string `json:"status"`
	}{PublicationResult: result, Status: "PUBLISHED"})
}

func loadInput(repositoryRoot, releaseID, recipientsFile string) (
	publication.SourceRelease,
	[]distribution.Recipient,
	error,
) {
	source, err := publication.DiscoverSource(repositoryRoot, releaseID)
	if err != nil {
		return publication.SourceRelease{}, nil, err
	}
	recipientFileInfo, err := os.Lstat(recipientsFile)
	if err != nil || !recipientFileInfo.Mode().IsRegular() || recipientFileInfo.Mode()&os.ModeSymlink != 0 {
		return publication.SourceRelease{}, nil, publication.ErrSource
	}
	file, err := os.Open(recipientsFile)
	if err != nil {
		return publication.SourceRelease{}, nil, publication.ErrSource
	}
	defer file.Close()
	recipients, err := publication.ParseRecipients(file)
	if err != nil {
		return publication.SourceRelease{}, nil, err
	}
	return source, recipients, nil
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(true)
	return encoder.Encode(value)
}

func environmentMap(entries []string) map[string]string {
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		for index := 0; index < len(entry); index++ {
			if entry[index] == '=' {
				result[entry[:index]] = entry[index+1:]
				break
			}
		}
	}
	return result
}
