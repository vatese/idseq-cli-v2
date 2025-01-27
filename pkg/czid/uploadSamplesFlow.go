package czid

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/chanzuckerberg/czid-cli/pkg/upload"
)

func UploadSamplesFlow(
	sampleFiles map[string]SampleFiles,
	stringMetadata map[string]string,
	projectName string,
	metadataCSVPath string,
	workflow string,
	technology string,
	wetlabProtocol string,
	medakaModel string,
	clearLabs bool,
	disableBuffer bool,
) error {
	metadata := NewMetadata(stringMetadata)
	projectID, err := DefaultClient.GetProjectID(projectName)
	if err != nil {
		log.Fatal(err)
	}

	samplesMetadata := SamplesMetadata{}
	if metadataCSVPath != "" {
		samplesMetadata, err = CSVMetadata(metadataCSVPath)
		if err != nil {
			log.Fatal(err)
		}
		for sampleName := range samplesMetadata {
			if _, hasSampleName := sampleFiles[sampleName]; !hasSampleName {
				delete(samplesMetadata, sampleName)
			}
		}
	}
	for sampleName := range sampleFiles {
		if _, hasMetadata := samplesMetadata[sampleName]; !hasMetadata {
			samplesMetadata[sampleName] = NewMetadata(map[string]string{})
		}
	}
	for sampleName, m := range samplesMetadata {
		samplesMetadata[sampleName] = m.Fuse(metadata)
	}

	sampleNames := make([]string, 0, len(sampleFiles))
	for sampleName := range samplesMetadata {
		sampleNames = append(sampleNames, sampleName)
	}
	newSampleNames, err := DefaultClient.ValidateSampleNames(sampleNames, projectID)
	if err != nil {
		log.Fatal(err)
	}
	if len(sampleNames) != len(newSampleNames) {
		log.Fatal("error validating sample names")
	}
	for i := range sampleNames {
		if newSampleNames[i] != sampleNames[i] {
			samplesMetadata[newSampleNames[i]] = samplesMetadata[sampleNames[i]]
			delete(samplesMetadata, sampleNames[i])
			sampleFiles[newSampleNames[i]] = sampleFiles[sampleNames[i]]
			delete(sampleFiles, sampleNames[i])
		}
	}

	err = GeoSearchSuggestions(&samplesMetadata)
	if err != nil {
		log.Fatal(err)
	}
	err = DefaultClient.ValidateSamplesMetadata(projectID, samplesMetadata)
	if err != nil {
		if err.Error() == "metadata validation failed" {
			os.Exit(1)
		}
		log.Fatal(err)
	}

	samples, err := DefaultClient.CreateSamples(
		projectID,
		sampleFiles,
		samplesMetadata,
		workflow,
		technology,
		wetlabProtocol,
		medakaModel,
		clearLabs,
	)
	if err != nil {
		log.Fatal(err)
	}

	var credentials aws.Credentials
	for _, sample := range samples {
		credentials, err = DefaultClient.GetUploadCredentials(sample.ID)
		if err != nil {
			log.Fatal(err)
		}
		u := upload.NewUploader(credentials, disableBuffer)
		sF := sampleFiles[sample.Name]
		for _, inputFile := range sample.InputFiles {
			filename := ""
			if filepath.Base(sF.R1) == filepath.Base(inputFile.S3Path) {
				filename = sF.R1
			} else if filepath.Base(sF.R2) == filepath.Base(inputFile.S3Path) {
				filename = sF.R2
			} else if filepath.Base(sF.Single) == filepath.Base(inputFile.S3Path) {
				filename = sF.Single
			} else {
				filenames := []string{}
				if sF.R1 != "" {
					filenames = append(filenames, sF.R1)
				}
				if sF.R2 != "" {
					filenames = append(filenames, sF.R2)
				}
				if sF.Single != "" {
					filenames = append(filenames, sF.Single)
				}

				return fmt.Errorf("s3 path %s did not match any of %s", inputFile.S3Path, strings.Join(filenames, ", "))
			}
			err := u.UploadFile(filename, inputFile.S3Path, inputFile.MultipartUploadId)
			if err != nil {
				log.Fatal(err)
			}
		}
		err := DefaultClient.MarkSampleUploaded(sample.ID, sample.Name)
		if err != nil {
			log.Fatal(err)
		}
	}
	return nil
}
