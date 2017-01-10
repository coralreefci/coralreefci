package models

import (
	"coralreefci/engine/gateway/conflation"
	"coralreefci/utils"
)

// DOC: JohnFold gradually increases the training data by increments of 1/10th.
func (m *Model) JohnFold(issues []conflation.ExpandedIssue) string {
	utils.ModelSummary.Info("John Fold issues count: ", len(issues))
	finalScore := 0.00
	for i := 0.10; i < 0.90; i += 0.10 {
		split := int(Round(i * float64(len(issues))))
		score, matrix, distinct := m.FoldImplementation(issues[:split], issues[split:])
		modelRecoveryFile := utils.Config.DataCachesPath + "/JFold" + ToString(i*10.0) + ".model"
		m.GenerateRecoveryFile(modelRecoveryFile)
		utils.ModelSummary.Info("Loop: " + ToString(i*10.0) + ", Accuracy: " + ToString(score))
		matrix.classesEvaluation(distinct)
		finalScore += score
	}
	return ToString(Round(finalScore / 9.00))
}

// DOC: TwoFold splits data in half - alternating training on each half.
func (m *Model) TwoFold(issues []conflation.ExpandedIssue) string {
	utils.ModelSummary.Info("Two Fold issues count: ", len(issues))
	split := int(0.50 * float64(len(issues)))
	firstScore, firstMatrix, firstDistinct := m.FoldImplementation(issues[:split], issues[split:])
	utils.ModelSummary.Info("First half, Accuracy: " + ToString(firstScore))
	firstMatrix.classesEvaluation(firstDistinct)
	secondScore, secondMatrix, secondDistinct := m.FoldImplementation(issues[split:], issues[:split])
	utils.ModelSummary.Info("Second half, Accuracy: " + ToString(secondScore))
	secondMatrix.classesEvaluation(secondDistinct)
	score := firstScore + secondScore
	return ToString(Round(score / 2.00))
}

// DOC: TenFold trains on a rolling 1/10th chunk of the input data.
func (m *Model) TenFold(issues []conflation.ExpandedIssue) string {
	utils.ModelSummary.Info("Ten Fold issues count: ", len(issues))
	finalScore := 0.00
	start := 0
	for i := 0.10; i <= 1.00; i += 0.10 {
		end := int(Round(i * float64(len(issues))))
		segment := issues[start:end]
		remainder := []conflation.ExpandedIssue{}
		remainder = append(issues[:start], issues[end:]...)
		score, matrix, distinct := m.FoldImplementation(segment, remainder)
		utils.ModelSummary.Info("Loop: " + ToString(i*10.0) + ", Accuracy: " + ToString(score))
		matrix.classesEvaluation(distinct)
		finalScore += score
		start = end
	}
	return ToString(Round(finalScore / 10.00))
}

func (m *Model) FoldImplementation(train, test []conflation.ExpandedIssue) (float64, matrix, []string) {
	expected := []string{}
	predicted := []string{}

	for i := 0; i < len(test); i++ {
		if test[i].Issue.Assignees != nil {
			expected = append(expected, *test[i].Issue.Assignees[0].Login)
		} else {
			expected = append(expected, *test[i].PullRequest.User.Login)
		}
	}

	m.Learn(train)
	correct := 0
	for i := 0; i < len(test); i++ {
		utils.ModelSummary.Debug("Actual Assignee: ", *test[i].Issue.Assignees[0].Login)
		predictions := m.Predict(test[i])
		length := 5
		if len(predictions) < 5 {
			length = len(predictions)
		}
		for j := 0; j < length; j++ {
			if predictions[j] == *test[i].Issue.Assignees[0].Login {
				predicted = append(predicted, predictions[j])
				correct++
				break
			} else {
				predicted = append(predicted, predictions[0])
				break
			}
		}
	}

	mat, dist, err := m.BuildMatrix(expected, predicted)
	if err != nil {
		utils.ModelSummary.Panic(err)
	}
	return float64(correct) / float64(len(test)), mat, dist
}
