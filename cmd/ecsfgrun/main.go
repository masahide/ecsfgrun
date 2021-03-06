package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs/cloudwatchlogsiface"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ecs/ecsiface"
	"github.com/go-ini/ini"
	"github.com/kelseyhightower/envconfig"
	"github.com/mitchellh/go-homedir"
)

const (
	credPath = ".aws/credentials"
	confPath = ".aws/config"

	iniRoleARN    = "role_arn"
	iniSrcProfile = "source_profile"
	iniRegion     = "region"
	//appName       = "ecsfgrun"
)

type environments struct {
	AWSSharedCredentialsFile string        `envconfig:"AWS_SHARED_CREDENTIALS_FILE"`
	AWSConfigFile            string        `envconfig:"AWS_CONFIG_FILE"`
	AWSDefaultProfile        string        `envconfig:"AWS_DEFAULT_PROFILE"`
	AWSProfile               string        `envconfig:"AWS_PROFILE"`
	AWSDefaultRegion         string        `envconfig:"AWS_DEFAULT_REGION"`
	AWSRegion                string        `envconfig:"AWS_REGION"`
	OverrideEnvPrefix        string        `envconfig:"OVERRIDE_ENV_PREFIX" default:"ECSFGRUN_"`
	Home                     string        `envconfig:"HOME"`
	StartWait                time.Duration `envconfig:"START_WAIT" default:"40s"`
	ShowPending              bool          `envconfig:"SHOW_PENDING" default:"false"`
	PrintTime                bool          `envconfig:"PRINT_TIME" default:"false"`
	AssignPublicIP           bool          `envconfig:"PUBLICIP" default:"true"`
	Cluster                  string        `envconfig:"CLUSTER" desc:"If you do not specify a cluster, the default cluster is assumed"`
	LaunchType               string        `envconfig:"LAUNCHTYPE" default:"FARGATE"`
	SecurityGroups           []string      `envconfig:"SECGROUPS" desc:"Security groups of awsvpc network mode"`
	Subnets                  []string      `envconfig:"SUBNETS" desc:"Subnets of awsvpc network mode"`
	TaskDefinition           string        `envconfig:"TASKDEF" required:"false" desc:"The family and revision (family:revision ) or full ARN of the task definition to run."`
}

type profileConfig struct {
	RoleARN    string
	SrcProfile string
	Region     string
}

var (
	env     environments
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	showVersion := false
	showHelp := false
	flag.BoolVar(&showVersion, "version", false, "show version")
	flag.BoolVar(&showHelp, "h", false, "show help")
	flag.Parse()
	if showVersion {
		fmt.Printf("%s version %v, commit %v, built at %v\n", filepath.Base(os.Args[0]), version, commit, date)
		os.Exit(0)
	}
	if showHelp {
		flag.PrintDefaults()
		envconfig.Usage("", &env) // nolint errcheck
		os.Exit(0)
	}

	log.SetFlags(log.Lshortfile | log.LstdFlags)
	err := envconfig.Process("", &env)
	if err != nil {
		log.Fatal(err)
	}
	if len(env.Home) == 0 {
		env.Home, err = homedir.Dir()
		if err != nil {
			log.Fatal(err)
		}
	}
}

func main() {
	args := flag.Args()
	sess := session.Must(session.NewSession())
	conf, err := getProfileConfig(getProfileEnv())
	if err == nil && len(conf.SrcProfile) > 0 {
		sess = getStsSession(conf)
	}
	code, err := run(ecs.New(sess), cloudwatchlogs.New(sess), env, args)
	if err != nil {
		log.Println(err)
	}
	os.Exit(code)
}

func createStrSliceRef(s []string) []*string {
	res := make([]*string, len(s))
	for i := range s {
		res[i] = &s[i]
	}
	return res
}

func run(ecsSv ecsiface.ECSAPI, logsSv cloudwatchlogsiface.CloudWatchLogsAPI, env environments, cmdline []string) (int, error) {
	input, err := createRunParam(ecsSv, env, cmdline)
	if err != nil {
		return 1, err
	}
	task, err := runContainer(ecsSv, input)
	if err != nil {
		return 1, err
	}
	logGroup := "/ecs/" + getGroupID(env.TaskDefinition)
	logStream := "ecs/" + aws.StringValue(task.Containers[0].Name) + "/" + getTaskID(task.TaskArn)

	logReq := cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  &logGroup,
		LogStreamName: &logStream,
		StartFromHead: aws.Bool(true),
		//Limit:         aws.Int64(0),
	}
	ecsReq := ecs.DescribeTasksInput{
		Cluster: &env.Cluster,
		Tasks:   []*string{aws.String(getTaskID(task.TaskArn))},
	}
	return readLog(os.Stdout, logsSv, ecsSv, logReq, ecsReq, env)
}

func createRunParam(client ecsiface.ECSAPI, env environments, cmdline []string) (*ecs.RunTaskInput, error) {
	assignPublicIP := "DISABLED"
	if env.AssignPublicIP {
		assignPublicIP = "ENABLED"
	}
	input := ecs.RunTaskInput{
		NetworkConfiguration: &ecs.NetworkConfiguration{
			AwsvpcConfiguration: &ecs.AwsVpcConfiguration{
				AssignPublicIp: &assignPublicIP,
				SecurityGroups: createStrSliceRef(env.SecurityGroups),
				Subnets:        createStrSliceRef(env.Subnets),
			},
		},
		LaunchType:     &env.LaunchType,
		TaskDefinition: &env.TaskDefinition,
		Cluster:        &env.Cluster,
	}
	if env.LaunchType == "EC2" {
		input.NetworkConfiguration = nil
	}
	if len(cmdline) > 0 {
		definition, err := client.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{TaskDefinition: &env.TaskDefinition})
		if err != nil {
			return nil, err
		}
		var containerName *string
		for _, container := range definition.TaskDefinition.ContainerDefinitions {
			containerName = container.Name
		}
		input.Overrides = &ecs.TaskOverride{
			ContainerOverrides: []*ecs.ContainerOverride{
				{
					Command:     createCmd(cmdline),
					Environment: makeEnvs(env.OverrideEnvPrefix),
					Name:        containerName,
				},
			},
		}
	}
	return &input, nil
}

func makeEnvs(prefix string) []*ecs.KeyValuePair {
	envs := os.Environ()
	res := make([]*ecs.KeyValuePair, 0, len(envs))
	for _, env := range envs {
		if !strings.HasPrefix(env, prefix) {
			continue
		}
		v := strings.SplitN(env, "=", 2)
		if len(v) != 2 {
			continue
		}
		v[0] = strings.TrimPrefix(v[0], prefix)
		res = append(res, &ecs.KeyValuePair{Name: aws.String(v[0]), Value: aws.String(v[1])})
	}
	if len(res) > 0 {
		return res
	}
	return nil
}

func readLog(w io.Writer, logsSv cloudwatchlogsiface.CloudWatchLogsAPI, ecsSv ecsiface.ECSAPI, logReq cloudwatchlogs.GetLogEventsInput, ecsReq ecs.DescribeTasksInput, env environments) (int, error) {
	time.Sleep(env.StartWait)
	for {
		c, err := getContainerInfo(ecsSv, &ecsReq)
		//pp.Println("containerInfo:", c)
		if err != nil {
			return 2, err
		}
		time.Sleep(3 * time.Second)
		if aws.StringValue(c.LastStatus) == "PENDING" {
			if env.ShowPending {
				log.Printf("Task Status: %s", *c.LastStatus)
			}
			continue
		}
		next, err := getLogs(logsSv, w, logReq, env)
		if err != nil {
			log.Printf("getLogs err:%s", err)
		}
		if aws.StringValue(c.LastStatus) == "STOPPED" {
			return int(aws.Int64Value(c.ExitCode)), nil
		}
		logReq.NextToken = next
	}

}

func createCmd(line []string) []*string {
	res := make([]*string, len(line))
	for i := range line {
		res[i] = &line[i]
	}
	return res
}

// see: https://github.com/boto/botocore/blob/2f0fa46380a59d606a70d76636d6d001772d8444/botocore/session.py#L82
func getProfileEnv() (profile string) {
	if env.AWSDefaultProfile != "" {
		return env.AWSDefaultProfile
	}
	profile = env.AWSProfile
	if len(profile) <= 0 {
		profile = "default"
	}
	return
}

func getStsSession(conf profileConfig) *session.Session {
	sess := session.Must(session.NewSession(&aws.Config{Credentials: credentials.NewSharedCredentials(awsFilePath(env.AWSSharedCredentialsFile, credPath, env.Home), conf.SrcProfile)}))
	return session.Must(session.NewSession(&aws.Config{Credentials: stscreds.NewCredentials(sess, conf.RoleARN), Region: &conf.Region}))
}

func awsFilePath(filePath, defaultPath, home string) string {
	if filePath != "" {
		if filePath[0] == '~' {
			return filepath.Join(home, filePath[1:])
		}
		return filePath
	}
	if home == "" {
		return ""
	}

	return filepath.Join(home, defaultPath)
}
func getProfileConfig(profile string) (res profileConfig, err error) {
	res, err = getProfile(profile, confPath)
	if err != nil {
		return res, err
	}
	if len(res.SrcProfile) > 0 && len(res.RoleARN) > 0 {
		return res, err
	}
	return getProfile(profile, credPath)
}

func getProfile(profile, configFileName string) (res profileConfig, err error) {
	cnfPath := awsFilePath(env.AWSConfigFile, configFileName, env.Home)
	config, err := ini.Load(cnfPath)
	if err != nil {
		return res, fmt.Errorf("failed to load shared credentials file. err:%s", err)
	}
	sec, err := config.GetSection(profile)
	if err != nil {
		// reference code -> https://github.com/aws/aws-sdk-go/blob/fae5afd566eae4a51e0ca0c38304af15618b8f57/aws/session/shared_config.go#L173-L181
		sec, err = config.GetSection(fmt.Sprintf("profile %s", profile))
		if err != nil {
			return res, fmt.Errorf("not found ini section err:%s", err)
		}
	}
	res.RoleARN = sec.Key(iniRoleARN).String()
	res.SrcProfile = sec.Key(iniSrcProfile).String()
	res.Region = sec.Key(iniRegion).String()
	// see: https://github.com/boto/botocore/blob/2f0fa46380a59d606a70d76636d6d001772d8444/botocore/session.py#L83
	if len(env.AWSRegion) > 0 {
		res.Region = env.AWSRegion
	}
	if len(env.AWSDefaultRegion) > 0 {
		res.Region = env.AWSDefaultRegion
	}
	return res, nil
}

func getLogs(client cloudwatchlogsiface.CloudWatchLogsAPI, w io.Writer, input cloudwatchlogs.GetLogEventsInput, conf environments) (*string, error) {
	res := &cloudwatchlogs.GetLogEventsOutput{}
	var err error
	for {
		if input.NextToken != nil && res.NextForwardToken != nil && *res.NextForwardToken == *input.NextToken {
			return input.NextToken, nil
		}
		if res.NextForwardToken != nil {
			input.NextToken = res.NextForwardToken
		}
		res, err = client.GetLogEvents(&input)
		if err != nil {
			return nil, err
		}
		for _, event := range res.Events {
			t := ""
			if conf.PrintTime {
				t = time.Unix(*event.Timestamp, 0).Format(time.RFC3339) + " "
			}
			if _, err := io.WriteString(w, t+*event.Message+"\n"); err != nil {
				return nil, err
			}
		}
	}

}

func runContainer(client ecsiface.ECSAPI, input *ecs.RunTaskInput) (*ecs.Task, error) {
	if input.Count != nil && *input.Count > 1 {
		return nil, errors.New("The count must be 1")
	}
	res, err := client.RunTask(input)
	if err != nil {
		return nil, err
	}
	for _, task := range res.Failures {
		return nil, errors.New(task.String())
	}
	for _, task := range res.Tasks {
		if task == nil {
			continue
		}
		return task, nil
	}
	return nil, errTaskNotFound
}

var errTaskNotFound = errors.New("task not found")

func getContainerInfo(client ecsiface.ECSAPI, input *ecs.DescribeTasksInput) (*ecs.Container, error) {
	res, err := client.DescribeTasks(input)
	if err != nil {
		return nil, err
	}
	if len(res.Failures) != 0 {
		return nil, errTaskNotFound
	}
	if res.Tasks == nil {
		return nil, errTaskNotFound
	}
	for _, task := range res.Tasks {
		for i := range task.Containers {
			if task.Containers[i] == nil {
				continue
			}
			return task.Containers[i], nil
		}
	}
	return nil, errTaskNotFound
}

var (
	taskIDRe = regexp.MustCompile("task/([^/]+)$")
)

func getGroupID(TaskDefName string) string {
	return strings.SplitN(TaskDefName, ":", 2)[0]
}

func getTaskID(s *string) string {
	matches := taskIDRe.FindAllStringSubmatch(aws.StringValue(s), 1)
	if len(matches) < 1 {
		return ""
	}
	return matches[0][1]
}
