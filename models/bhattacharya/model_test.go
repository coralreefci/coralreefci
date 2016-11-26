package bhattacharya

import (
	"coralreefci/models/issues"
	"strings"
	"testing"
)

func TestModel(t *testing.T) {
	v := 1
	if v != 1 {
		t.Error(
			"\nFOR:      ", v,
			"\nEXPECTED: ", 1,
			"\nACTUAL:   ", v,
		)
	}
}

func CreateTrainingIssues() []issues.Issue {
	return []issues.Issue{
		issues.Issue{Body: "parallel test body", Assignee: "Mike"},
		issues.Issue{Body: "fxcore test body and", Assignee: "John"},
		issues.Issue{Body: "fxcore test body or", Assignee: "John"},
		issues.Issue{Body: "something or other whatever", Assignee: "Mike"},
		issues.Issue{Body: "Woz and Jobs are cool", Assignee: "Mike"},
		issues.Issue{Body: "Hendricks and Bachman are better", Assignee: "John"},
		issues.Issue{Body: "name a shrub after me", Assignee: "Mike"},
	}
}

func CreateValidationIssues() []issues.Issue {
	return []issues.Issue{
		issues.Issue{Body: "parallel test code", Assignee: "Mike"},
		issues.Issue{Body: "fxcore test code and", Assignee: "Mike"},
		issues.Issue{Body: "fxcore test code or", Assignee: "Mike"},
		issues.Issue{Body: "something or other whatever", Assignee: "Mike"},
		issues.Issue{Body: "Woz and Jobs are cool", Assignee: "Mike"},
		issues.Issue{Body: "Hendricks and Bachman are better", Assignee: "John"},
		issues.Issue{Body: "name a shrub after me", Assignee: "Mike"},
	}
}

func CreateUnassignedIssues() []issues.Issue {
	return []issues.Issue{
		issues.Issue{Body: "parallel test code"},
		issues.Issue{Body: "fxcore test code and"},
		issues.Issue{Body: "fxcore test code or"},
		issues.Issue{Body: "something or other whatever"},
		issues.Issue{Body: "Woz and Jobs are cool"},
		issues.Issue{Body: "Hendricks and Bachman are better"},
		issues.Issue{Body: "name a shrub after me"},
	}
}

func TestLearn(t *testing.T) {
	nbModel := Model{Classifier: &NBClassifier{}}
	// logger := CreateLog("unit-test-model")
	// nbModel := Model{Classifier: &NBClassifier{Logger: &logger}, Logger: &logger}
	trainingSet := CreateTrainingIssues()
	validationSet := CreateValidationIssues()

	nbModel.Learn(trainingSet)

	for i := 0; i < len(validationSet); i++ {
		input := validationSet[i].Body
		expected := validationSet[i].Assignee
		actual := nbModel.Predict(validationSet[i])
		AssertList(t, expected, actual, input)
	}
}

func Assert(t *testing.T, expected string, actual string, input string) {
	if actual != expected {
		t.Error(
			"\nFOR:       ", input,
			"\nEXPECTED:  ", expected,
			"\nACTUAL:    ", actual,
		)
	}
}

func AssertList(t *testing.T, expected string, actual []string, input string) {
	for i := 0; i < len(actual); i++ {
		if actual[i] == expected {
			return
		}
	}
	t.Error(
		"\nFOR:       ", input,
		"\nEXPECTED:  ", expected,
		"\nACTUAL:    ", strings.Join(actual, ","),
	)
}
