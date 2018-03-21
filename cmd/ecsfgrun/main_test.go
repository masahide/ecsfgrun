package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs/cloudwatchlogsiface"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ecs/ecsiface"
)

const (
	testHomeA = "./test/a"
	testHomeB = "./test/b"
	awsCred   = ".aws/credentials"
	awsConf   = ".aws/config"
)

func TestGetProfileEnv(t *testing.T) {
	var vtests = []struct {
		defValue  string
		profValue string
		expected  string
	}{
		{"def", "prof", "def"},
		{"", "prof", "prof"},
	}
	for _, vt := range vtests {
		env.AWSDefaultProfile = vt.defValue
		env.AWSProfile = vt.profValue
		r := getProfileEnv()
		if r != vt.expected {
			t.Errorf("AWSDefaultProfile=%q,AWSProfile=%q,getProfileEnv() = %q, want %q", vt.defValue, vt.profValue, r, vt.expected)
		}
	}
}

func TestAwsFilePath(t *testing.T) {
	var vtests = []struct {
		envValue         string
		defaultPathParam string
		expected         string
	}{
		{
			envValue:         filepath.Join("~", awsCred),
			defaultPathParam: awsCred,
			expected:         filepath.Join(testHomeA, awsCred),
		}, {
			envValue:         filepath.Join("~", awsConf),
			defaultPathParam: awsConf,
			expected:         filepath.Join(testHomeA, awsConf),
		}, {
			envValue:         "",
			defaultPathParam: ".aws/credentials",
			expected:         filepath.Join(testHomeA, awsCred),
		}, {
			envValue:         "",
			defaultPathParam: awsConf,
			expected:         filepath.Join(testHomeA, awsConf),
		},
	}

	env.Home = testHomeA
	for _, vt := range vtests {
		r := awsFilePath(vt.envValue, vt.defaultPathParam, testHomeA)
		if r != vt.expected {
			t.Errorf("awsFilePath(%q, %q) = %q, want %q", vt.envValue, vt.defaultPathParam, r, vt.expected)
		}
	}
}

func TestGetProfileConfig(t *testing.T) {
	var vtests = []struct {
		home      string
		profile   string
		envRegion string
		err       *string
		expected  profileConfig
	}{
		{
			testHomeA,
			"testprof",
			"",
			nil,
			profileConfig{
				RoleARN:    "arn:aws:iam::123456789012:role/Admin",
				Region:     "ap-northeast-1",
				SrcProfile: "srcprof",
				//SrcRegion:    "us-east-1",
				//SrcAccountID: "000000000000",
			},
		},
		{
			testHomeB,
			"not_profile_prefix",
			"",
			nil,
			profileConfig{
				RoleARN:    "arn:aws:iam::123456789011:role/a",
				Region:     "ap-northeast-1",
				SrcProfile: "srcprof",
				//SrcRegion:    "us-east-1",
				//SrcAccountID: "000000000000",
			},
		},
		{
			testHomeB,
			"src_default",
			"",
			nil,
			profileConfig{
				RoleARN:    "arn:aws:iam::123456789011:role/b",
				Region:     "ap-northeast-1",
				SrcProfile: "default",
				//SrcRegion:    "us-east-1",
				//SrcAccountID: "000000000000",
			},
		},
		{
			testHomeB,
			"none",
			"",
			aws.String("not found ini section err:section 'profile none' does not exist"),
			profileConfig{
				RoleARN:    "",
				Region:     "",
				SrcProfile: "",
				//SrcRegion:    "us-east-1",
				//SrcAccountID: "000000000000",
			},
		},
		{
			testHomeB,
			"none",
			"ap-northeast-1",
			aws.String("not found ini section err:section 'profile none' does not exist"),
			profileConfig{
				RoleARN:    "",
				Region:     "",
				SrcProfile: "",
				//SrcRegion:    "us-east-1",
				//SrcAccountID: "000000000000",
			},
		},
	}
	for _, vt := range vtests {
		env.Home = vt.home
		env.AWSDefaultRegion = vt.envRegion
		res, err := getProfileConfig(vt.profile)
		if err != nil && vt.err == nil {
			t.Errorf("err getProfileConfig(%q) = err:%s", vt.profile, err)
		}
		if err != nil {
			if err.Error() != *vt.err {
				t.Errorf("err getProfileConfig(%q) = err:%s", vt.profile, err)
			}
		}
		if res != vt.expected {
			t.Errorf("getProfileConfig(%q); = %q, want %q", vt.profile, res, vt.expected)
		}
	}
}

type mockedCWL struct {
	cloudwatchlogsiface.CloudWatchLogsAPI
	resp cloudwatchlogs.GetLogEventsOutput
	err  error
}

func (m mockedCWL) GetLogEvents(in *cloudwatchlogs.GetLogEventsInput) (*cloudwatchlogs.GetLogEventsOutput, error) {
	// Only need to return mocked response output
	if in.NextToken != nil {
		m.resp.Events = []*cloudwatchlogs.OutputLogEvent{}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &m.resp, nil
}

func TestGetLogs(t *testing.T) {
	var vtests = []struct {
		input cloudwatchlogs.GetLogEventsInput
		conf  environments
		resp  cloudwatchlogs.GetLogEventsOutput
		err   error
	}{
		{
			cloudwatchlogs.GetLogEventsInput{
				LogGroupName:  aws.String(""),
				LogStreamName: aws.String(""),
			},
			environments{PrintTime: true},
			cloudwatchlogs.GetLogEventsOutput{
				Events: []*cloudwatchlogs.OutputLogEvent{
					{
						Timestamp: aws.Int64(1519556892),
						Message:   aws.String("sample message log........"),
					},
					{
						Timestamp: aws.Int64(1519556893),
						Message:   aws.String("sample message log2........"),
					},
				},
				NextForwardToken: aws.String("hogehoge"),
			},
			nil,
		},
		{
			cloudwatchlogs.GetLogEventsInput{},
			environments{},
			cloudwatchlogs.GetLogEventsOutput{},
			errors.New("test error"),
		},
	}
	for i, vt := range vtests {
		var b bytes.Buffer
		m := mockedCWL{
			resp: vt.resp,
			err:  vt.err,
		}
		_, err := getLogs(&m, &b, vt.input, vt.conf)
		if err != vt.err {
			t.Errorf("err %d:getLogs() = err:%s, want:%s", i, err, vt.err)
		}
		lines := strings.Split(b.String(), "\n")
		for j, line := range lines {
			if len(line) == 0 {
				break
			}
			s := strings.SplitN(line, " ", 2)
			mes := line
			if j > len(vt.resp.Events) {
				t.Errorf("err len(split(line, ' '))%d > len(vt.resp.Events)%d s=%s", j, len(vt.resp.Events), s)
			}
			if vt.conf.PrintTime {
				_, err := time.Parse(time.RFC3339, s[0])
				if err != nil {
					t.Errorf("err %d:getLogs() = %s, PrintTime:%v", i, line, vt.conf.PrintTime)
				}
				mes = s[1]
			}
			if mes != *vt.resp.Events[j].Message {
				t.Errorf("err %d:getLogs() = %s, want:%s", i, mes, *vt.resp.Events[j].Message)
			}

		}
	}

}

type mockedECS struct {
	ecsiface.ECSAPI
	rtresp  ecs.RunTaskOutput
	dtresp  ecs.DescribeTasksOutput
	dtdresp ecs.DescribeTaskDefinitionOutput
	err     error
}

func (m *mockedECS) RunTask(input *ecs.RunTaskInput) (*ecs.RunTaskOutput, error) {
	return &m.rtresp, m.err
}
func (m *mockedECS) DescribeTaskDefinition(input *ecs.DescribeTaskDefinitionInput) (*ecs.DescribeTaskDefinitionOutput, error) {
	return &m.dtdresp, m.err
}

func TestRunTask(t *testing.T) {
	var vtests = []struct {
		input       ecs.RunTaskInput
		rtresp      ecs.RunTaskOutput
		dtdresp     ecs.DescribeTaskDefinitionOutput
		err         error
		expected    string
		expectedErr string
	}{
		{
			ecs.RunTaskInput{
				Cluster: aws.String(""),
			},
			ecs.RunTaskOutput{
				Failures: []*ecs.Failure{
					{Arn: aws.String("arn"), Reason: aws.String("reason")},
					{Arn: aws.String("arn"), Reason: aws.String("reason")},
				},
				Tasks: []*ecs.Task{
					{
						TaskArn: aws.String("arn"),
						Containers: []*ecs.Container{
							{
								Name:       aws.String("hoge"),
								LastStatus: aws.String("STOPPED"),
								ExitCode:   aws.Int64(0),
							},
						},
					},
				},
			},
			ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecs.TaskDefinition{
					ContainerDefinitions: []*ecs.ContainerDefinition{
						&ecs.ContainerDefinition{
							Name: aws.String("hoge"),
						},
					},
				},
			},
			nil,
			"",
			"{\n  Arn: \"arn\",\n  Reason: \"reason\"\n}",
		},
		{
			ecs.RunTaskInput{},
			ecs.RunTaskOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn: aws.String("arn"),
						Containers: []*ecs.Container{
							{
								Name:       aws.String("hoge"),
								LastStatus: aws.String("STOPPED"),
								ExitCode:   aws.Int64(0),
							},
						},
					},
				},
			},
			ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecs.TaskDefinition{
					ContainerDefinitions: []*ecs.ContainerDefinition{
						&ecs.ContainerDefinition{
							Name: aws.String("hoge"),
						},
					},
				},
			},
			errors.New("test error"),
			"",
			"test error",
		},
	}
	for i, vt := range vtests {
		//func runContainer(client ecsiface.ECSAPI, input *ecs.RunTaskInput) (string, error) {
		m := mockedECS{
			rtresp:  vt.rtresp,
			dtdresp: vt.dtdresp,
			err:     vt.err,
		}
		res, err := runContainer(&m, &vt.input)
		if err != nil {
			if err.Error() != vt.expectedErr {
				t.Errorf("err %d:runContainer() = err:%s, want:%s", i, err, vt.expectedErr)
			}
			continue
		}
		if aws.StringValue(res.TaskArn) != vt.expected {
			t.Errorf("err %d:runContainer() = %s, want:%s", i, aws.StringValue(res.TaskArn), vt.expected)
		}
	}
}

func (m *mockedECS) DescribeTasks(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error) {
	return &m.dtresp, m.err
}

func TestGetContainerInfo(t *testing.T) {
	var vtests = []struct {
		input        ecs.DescribeTasksInput
		dtresp       ecs.DescribeTasksOutput
		err          error
		expected     string
		expectedCode int64
		expectedErr  string
	}{
		{
			ecs.DescribeTasksInput{},
			ecs.DescribeTasksOutput{
				Failures: []*ecs.Failure{
					{
						Reason: aws.String("MISSING"),
						Arn:    aws.String("arn:aws:ecs:us-east-1:954586889057:task/305b887f-2881-6b26-a443-6441f4443b73"),
					},
				},
			},
			nil,
			"",
			int64(0),
			"task not found",
		},
		{
			ecs.DescribeTasksInput{},
			ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn: aws.String("arn"),
						Containers: []*ecs.Container{
							{
								LastStatus: aws.String("STOPPED"),
								ExitCode:   aws.Int64(0),
							},
						},
					},
				},
			},
			nil,
			"STOPPED",
			int64(0),
			"",
		},
	}
	for i, vt := range vtests {
		//func runContainer(client ecsiface.ECSAPI, input *ecs.RunTaskInput) (string, error) {
		m := mockedECS{
			dtresp: vt.dtresp,
			err:    vt.err,
		}

		container, err := getContainerInfo(&m, &vt.input)
		if err != nil {
			if err.Error() != vt.expectedErr {
				t.Errorf("err %d:getContainerInfo() = err:%s, want:%s", i, err, vt.expectedErr)
			}
			continue
		}
		if *container.LastStatus != vt.expected {
			t.Errorf("err %d:getContainerInfo() = %s, want:%s", i, *container.LastStatus, vt.expected)
		}
		if *container.ExitCode != vt.expectedCode {
			t.Errorf("err %d:getContainerInfo() = code:%d, want:%d", i, *container.ExitCode, vt.expectedCode)
		}
	}
}

func TestMakeRefStrSlice(t *testing.T) {
	var vtests = []struct {
		input    []string
		expected []*string
	}{
		{
			[]string{"hoge", "fuga"},
			[]*string{aws.String("hoge"), aws.String("fuga")},
		},
		{
			[]string{"fuga"},
			[]*string{aws.String("fuga")},
		},
		{
			[]string{},
			[]*string{},
		},
	}
	for _, vt := range vtests {
		res := createStrSliceRef(vt.input)
		for i := range res {
			if *res[i] != vt.input[i] {
				t.Errorf("createStrSliceRef(%v) = %#v, want:%#v", vt.input, res, vt.expected)
			}
		}
	}
}

func TestGetTaskID(t *testing.T) {
	var vtests = []struct {
		input    string
		expected string
	}{
		{
			"arn:aws:ecs:us-east-1:954586889057:task/305b887f-2881-6b26-a443-6441f4443b73",
			"305b887f-2881-6b26-a443-6441f4443b73",
		},
	}
	for _, vt := range vtests {
		res := getTaskID(aws.String(vt.input))
		if res != vt.expected {
			t.Errorf("getTaskID(%v) = %#v, want:%#v", vt.input, res, vt.expected)
		}
	}
}

func TestMakeEnvs(t *testing.T) {
	os.Setenv("hogefuga_hoge", "hogehoge")
	os.Setenv("hogefuga_fuga", "fugafuga")
	os.Setenv("testENV", "hogehoge")
	kvs := makeEnvs("hogefuga_")
	v, err := getEnv(kvs, "hoge")
	if err != nil {
		t.Error(err)
	}
	if v != "hogehoge" {
		t.Errorf("err value: %s", v)
	}
	if _, err := getEnv(kvs, "testEnv"); err == nil {
		t.Error("error")
	}

	if makeEnvs("fugafugafuadfa") != nil {
		t.Error("not empty")
	}
}

func getEnv(kvs []*ecs.KeyValuePair, key string) (string, error) {
	for _, kv := range kvs {
		if *kv.Name == key {
			return *kv.Value, nil
		}
	}
	return "", fmt.Errorf("%s not found", key)
}

func TestGetGroupID(t *testing.T) {
	var vtests = []struct {
		input    string
		expected string
	}{
		{"hoge:latest", "hoge"},
		{"aaa:1", "aaa"},
		{"fuga", "fuga"},
	}
	for _, vt := range vtests {
		res := getGroupID(vt.input)
		if res != vt.expected {
			t.Errorf("getGroupID(%v) = %#v, want:%#v", vt.input, res, vt.expected)
		}
	}
}

func TestRun(t *testing.T) {
	var vtests = []struct {
		lresp    cloudwatchlogs.GetLogEventsOutput
		lerr     error
		rtresp   ecs.RunTaskOutput
		dtresp   ecs.DescribeTasksOutput
		dtdresp  ecs.DescribeTaskDefinitionOutput
		derr     error
		args     []string
		err      error
		expected int
	}{
		{
			cloudwatchlogs.GetLogEventsOutput{
				Events: []*cloudwatchlogs.OutputLogEvent{
					{
						Timestamp: aws.Int64(1519556892),
						Message:   aws.String("sample message log........"),
					},
					{
						Timestamp: aws.Int64(1519556893),
						Message:   aws.String("sample message log2........"),
					},
				},
				NextForwardToken: aws.String("hogehoge"),
			},
			nil,
			ecs.RunTaskOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn: aws.String("arn"),
						Containers: []*ecs.Container{
							{
								Name:       aws.String("hoge"),
								LastStatus: aws.String("STOPPED"),
								ExitCode:   aws.Int64(0),
							},
						},
					},
				},
			},
			ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn: aws.String("arn"),
						Containers: []*ecs.Container{
							{
								Name:       aws.String("hoge"),
								LastStatus: aws.String("STOPPED"),
								ExitCode:   aws.Int64(0),
							},
						},
					},
				},
				Failures: []*ecs.Failure{
					{
						Reason: aws.String("MISSING"),
						Arn:    aws.String("arn:aws:ecs:us-east-1:954586889057:task/305b887f-2881-6b26-a443-6441f4443b73"),
					},
				},
			},
			ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecs.TaskDefinition{
					ContainerDefinitions: []*ecs.ContainerDefinition{
						&ecs.ContainerDefinition{
							Name: aws.String("hoge"),
						},
					},
				},
			},
			nil,
			[]string{},
			errTaskNotFound,
			2,
		},
		{
			cloudwatchlogs.GetLogEventsOutput{
				Events: []*cloudwatchlogs.OutputLogEvent{
					{
						Timestamp: aws.Int64(1519556892),
						Message:   aws.String("sample message log........"),
					},
					{
						Timestamp: aws.Int64(1519556893),
						Message:   aws.String("sample message log2........"),
					},
				},
				NextForwardToken: aws.String("hogehoge"),
			},
			nil,
			ecs.RunTaskOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn:    aws.String("arn"),
						Containers: []*ecs.Container{&ecs.Container{Name: aws.String("hoge")}},
					},
				},
			},
			ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn: aws.String("arn"),
						Containers: []*ecs.Container{
							{
								Name:       aws.String("hoge"),
								LastStatus: aws.String("STOPPED"),
								ExitCode:   aws.Int64(0),
							},
						},
					},
				},
			},
			ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecs.TaskDefinition{
					ContainerDefinitions: []*ecs.ContainerDefinition{
						&ecs.ContainerDefinition{
							Name: aws.String("hoge"),
						},
					},
				},
			},
			nil,
			[]string{},
			nil,
			0,
		},
		{
			cloudwatchlogs.GetLogEventsOutput{
				Events: []*cloudwatchlogs.OutputLogEvent{
					{
						Timestamp: aws.Int64(1519556892),
						Message:   aws.String("sample message log........"),
					},
					{
						Timestamp: aws.Int64(1519556893),
						Message:   aws.String("sample message log2........"),
					},
				},
				NextForwardToken: aws.String("hogehoge"),
			},
			nil,
			ecs.RunTaskOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn:    aws.String("arn"),
						Containers: []*ecs.Container{&ecs.Container{Name: aws.String("hoge")}},
					},
				},
			},
			ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{
					{
						TaskArn: aws.String("arn"),
						Containers: []*ecs.Container{
							{
								Name:       aws.String("hoge"),
								LastStatus: aws.String("STOPPED"),
								ExitCode:   aws.Int64(0),
							},
						},
					},
				},
			},
			ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecs.TaskDefinition{
					ContainerDefinitions: []*ecs.ContainerDefinition{
						&ecs.ContainerDefinition{
							Name: aws.String("hoge"),
						},
					},
				},
			},
			nil,
			[]string{"hoge", "fuga"},
			nil,
			0,
		},
	}
	env.StartWait = 0
	for i, vt := range vtests {
		tm := mockedECS{
			dtresp:  vt.dtresp,
			err:     vt.derr,
			rtresp:  vt.rtresp,
			dtdresp: vt.dtdresp,
		}
		lm := mockedCWL{
			resp: vt.lresp,
			err:  vt.lerr,
		}

		code, err := run(&tm, &lm, env, vt.args)
		if err != vt.err {
			t.Errorf("err %d:run() = err:%s, want:%s", i, err, vt.err)
		}
		if code != vt.expected {
			t.Errorf("err %d:run() = %d, want:%d", i, code, vt.expected)
		}
	}
}
func TestCreateCmd(t *testing.T) {
	var vtests = []struct {
		line     []string
		expected []string
	}{
		{[]string{"hoge", "fuga"}, []string{"hoge", "fuga"}},
	}
	for i, vt := range vtests {
		res := createCmd(vt.line)
		for j := range res {
			if *res[j] != vt.expected[j] {
				t.Errorf("err %d:getStsSession() = %#v, want:%#v", i, res, vt.expected)
			}
		}
	}

}
func TestGetStsSession(t *testing.T) {
	var vtests = []struct {
		conf     profileConfig
		expected *string
	}{
		{profileConfig{}, nil},
	}
	os.Unsetenv("AWS_DEFAULT_PROFILE")
	os.Unsetenv("AWS_PROFILE")
	os.Unsetenv("AWS_REGION")
	os.Unsetenv("AWS_DEFAULT_REGION")
	os.Unsetenv("AWS_SHARED_CREDENTIALS_FILE")
	os.Unsetenv("AWS_CONFIG_FILE")
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_SESSION_TOKEN")
	env.Home = "/dev/null"
	for i, vt := range vtests {
		res := getStsSession(vt.conf)
		if res.Config.Endpoint != vt.expected {
			t.Errorf("err %d:getStsSession() = %#v, want:%#v", i, res.Config.Endpoint, vt.expected)
		}
	}
}
