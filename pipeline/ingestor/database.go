package ingestor

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/go-github/github"
	"go.uber.org/zap"

	"core/utils"
)

type Event struct {
	Type    string            `json:"type"`
	Repo    github.Repository `json:"repo"`
	Action  string            `json:"action"`
	Payload interface{}       `json:"payload"`
}

type Value interface{}

type Integration struct {
	RepoId         int
	AppId          int
	InstallationId int
}

type SQLDB interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Exec(query string, args ...interface{}) (sql.Result, error)
	Close() error
}

type Database struct {
	db         SQLDB // *sql.DB
	BufferPool Pool
}

type EventType int

const (
	PullRequest EventType = iota
	Issue
	All
)

type EventQuery struct {
	Type EventType
	Repo string
}

func (d *Database) Open() {
	mysql, err := sql.Open("mysql", "root@/heupr?interpolateParams=true")
	if err != nil {
		panic(err.Error()) // Just for example purpose. You should use proper error handling instead of panic
	}
	d.db = mysql
}

func (d *Database) Close() {
	d.db.Close()
}

func (d *Database) FlushBackTestTable() {
	d.db.Exec("optimize table backtest_events flush")
}

func (d *Database) EnableRepo(repoId int) {
	var buffer bytes.Buffer
	archRepoInsert := "INSERT INTO arch_repos(repository_id, enabled) VALUES"
	valuesFmt := "(?,?)"

	buffer.WriteString(archRepoInsert)
	buffer.WriteString(valuesFmt)
	result, err := d.db.Exec(buffer.String(), repoId, true)
	if err != nil {
		utils.AppLog.Error("database repo insert failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("database repo insert success", zap.Int64("rows", rows))
	}
}

func (d *Database) InsertRepositoryIntegration(repoId int, appId int, installationId int) {
	var buffer bytes.Buffer
	integrationsInsert := "INSERT INTO integrations(repo_id, app_id, installation_id) VALUES"
	valuesFmt := "(?,?,?)"

	buffer.WriteString(integrationsInsert)
	buffer.WriteString(valuesFmt)
	result, err := d.db.Exec(buffer.String(), repoId, appId, installationId)
	if err != nil {
		utils.AppLog.Error("database integration insert failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("database integration insert success", zap.Int64("rows", rows))
	}
}

func (d *Database) DeleteRepositoryIntegration(repoId int, appId int, installationId int) {
	result, err := d.db.Exec("DELETE FROM integrations where repo_id = ? and app_id = ? and installation_id = ?", repoId, appId, installationId)
	if err != nil {
		utils.AppLog.Error("database integration delete failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("database integration delete success", zap.Int64("rows", rows))
	}
}

func (d *Database) ObliterateIntegration(appId int, installationId int) {
	result, err := d.db.Exec("DELETE FROM integrations where app_id = ? and installation_id = ?", appId, installationId)
	if err != nil {
		utils.AppLog.Error("database integration obliterate failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("database integration obliterate success", zap.Int64("rows", rows))
	}
}

func (d *Database) ReadIntegrations() ([]Integration, error) {
	integrations := []Integration{}
	results, err := d.db.Query("SELECT repo_id, app_id, installation_id FROM integrations")
	if err != nil {
		return nil, err
	}

	defer results.Close()
	for results.Next() {
		integration := Integration{}
		err := results.Scan(&integration.RepoId, &integration.AppId, &integration.InstallationId)
		if err != nil {
			return nil, err
		}
		integrations = append(integrations, integration)
		err = results.Err()
		if err != nil {
			return nil, err
		}
	}
	return integrations, nil
}

func (d *Database) ReadIntegrationByRepoId(repoId int) (*Integration, error) {
	integration := new(Integration)
	err := d.db.QueryRow("SELECT repo_id, app_id, installation_id FROM integrations WHERE repo_id = ?", repoId).Scan(&integration.RepoId, &integration.AppId, &integration.InstallationId)
	if err != nil {
		utils.AppLog.Error("database read failure - ReadIntegrationByRepoId()", zap.Error(err))
		return nil, err
	}
	return integration, nil
}

func (d *Database) BulkInsertBacktestEvents(events []*Event) {
	buffer := d.BufferPool.Get()
	for i := 0; i < len(events); i++ {
		buffer.AppendInt(int64(*events[i].Repo.ID))
		buffer.AppendByte('~')
		buffer.AppendString(*events[i].Repo.Name)
		buffer.AppendByte('~')
		if events[i].Action == "closed" {
			buffer.AppendInt(1)
		} else {
			buffer.AppendInt(0)
		}
		buffer.AppendByte('~')
		if events[i].Type == "PullRequestEvent" {
			buffer.AppendInt(1)
		} else {
			buffer.AppendInt(0)
		}
		buffer.AppendByte('~')
		payload, _ := json.Marshal(events[i])
		_, _ = buffer.Write(escapeBytesBackslash(stripCtlAndExtFromBytes(payload)))
		buffer.AppendByte('\n')
	}

	sqlBuffer := bytes.NewBuffer(buffer.Bytes())
	buffer.Reset()
	buffer.Free()

	mysql.RegisterReaderHandler("data", func() io.Reader {
		return sqlBuffer
	})
	defer mysql.DeregisterReaderHandler("data")
	result, err := d.db.Exec("LOAD DATA LOCAL INFILE 'Reader::data' INTO TABLE backtest_events FIELDS TERMINATED BY '~' LINES TERMINATED BY '\n' (repo_id,repo_name,is_closed,is_pull,payload)")
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("database insert success", zap.Int64("rows", rows))
	}
	sqlBuffer.Reset()
}

func (d *Database) ReadBacktestEvents(params EventQuery) ([]Event, error) {
	events := []Event{}
	var payload []byte
	var results *sql.Rows
	var err error
	switch t := params.Type; t {
	case PullRequest:
		results, err = d.db.Query("select payload from backtest_events where repo_name=? and is_pull=? and is_closed=?", params.Repo, 1, 1)
	case Issue:
		results, err = d.db.Query("select payload from backtest_events where repo_name=? and is_pull=? and is_closed=?", params.Repo, 0, 1)
	default:
		results, err = d.db.Query("select payload from backtest_events where repo_name=? and is_closed=?", params.Repo, 1)
	}
	if err != nil {
		return nil, err
	}
	defer results.Close()
	for results.Next() {
		var event Event
		err := results.Scan(&payload)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if err := decoder.Decode(&event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	err = results.Err()
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (d *Database) ReadBacktestRepos() ([]github.Repository, error) {
	repos := []github.Repository{}

	results, err := d.db.Query(`select T.repo_name, T.repo_id
	from
	(
		select count(*) cnt, repo_name, repo_id from backtest_events where is_pull = 0 and is_closed = 1 and repo_name != 'chrsmith/google-api-java-client'
		group by repo_name
	) T
	order by T.cnt desc LIMIT 10
    `)
	if err != nil {
		return nil, err
	}
	defer results.Close()

	for results.Next() {
		repo_name := new(string)
		repo_id := new(int)
		if err := results.Scan(repo_name, repo_id); err != nil {
			return nil, err
		}
		r := strings.Split(*repo_name, "/")
		repos = append(repos, github.Repository{ID: repo_id, Name: github.String(r[1]), Organization: &github.Organization{Name: github.String(r[0])}})
	}

	err = results.Err()
	if err != nil {
		return nil, err
	}
	return repos, nil
}

func (d *Database) ReadIssuesTest() ([]github.Issue, error) {
	events := []github.Issue{}
	var payload []byte
	var results *sql.Rows
	var err error
	results, err = d.db.Query("select payload from github_events where is_pull=0")
	if err != nil {
		return nil, err
	}
	defer results.Close()
	for results.Next() {
		var event github.Issue
		err := results.Scan(&payload)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if err := decoder.Decode(&event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	err = results.Err()
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (d *Database) ReadPullRequestTest() ([]github.PullRequest, error) {
	events := []github.PullRequest{}
	var payload []byte
	var results *sql.Rows
	var err error
	results, err = d.db.Query("select payload from github_events where is_pull=1")
	if err != nil {
		return nil, err
	}
	defer results.Close()
	for results.Next() {
		var event github.PullRequest
		err := results.Scan(&payload)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if err := decoder.Decode(&event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	err = results.Err()
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (d *Database) InsertIssue(issue github.Issue, action *string) {
	d.LogIssueAssignees(issue)

	var buffer bytes.Buffer
	eventsInsert := "INSERT INTO github_events(repo_id,issues_id,number,action,payload,is_pull,is_closed,closed_at) VALUES"
	eventsValuesFmt := "(?,?,?,?,?,0,?,?)"
	numValues := 7

	buffer.WriteString(eventsInsert)
	buffer.WriteString(eventsValuesFmt)
	values := make([]interface{}, numValues)
	values[0] = *issue.Repository.ID
	values[1] = issue.ID
	values[2] = issue.Number
	values[3] = action
	payload, _ := json.Marshal(issue)
	values[4] = stripCtlAndExtFromBytes(payload)
	if issue.ClosedAt == nil {
		values[5] = false
	} else {
		values[5] = true
	}
	values[6] = issue.ClosedAt
	result, err := d.db.Exec(buffer.String(), values...)
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Debug("Database Insert Success", zap.Int64("Rows", rows))
	}
}

func (d *Database) BulkInsertIssuesPullRequests(issues []*github.Issue, pulls []*github.PullRequest) {
	buffer := d.BufferPool.Get()

	for i := 0; i < len(issues); i++ {
		d.LogIssueAssignees(*issues[i])

		buffer.AppendInt(int64(*issues[i].Repository.ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*issues[i].ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*issues[i].Number))
		buffer.AppendByte('~')
		if issues[i].ClosedAt == nil {
			buffer.AppendString("opened")
			buffer.AppendByte('~')
		} else {
			buffer.AppendString("closed")
			buffer.AppendByte('~')
		}
		payload, _ := json.Marshal(*issues[i])
		_, _ = buffer.Write(escapeBytesBackslash(stripCtlAndExtFromBytes(payload)))
		buffer.AppendByte('~')
		buffer.AppendInt(0)
		buffer.AppendByte('~')
		if issues[i].ClosedAt == nil {
			buffer.AppendInt(0)
			buffer.AppendByte('~')
		} else {
			buffer.AppendInt(1)
			buffer.AppendByte('~')
			buffer.Write([]byte(issues[i].ClosedAt.Format(time.RFC3339Nano)))
		}
		buffer.AppendByte('\n')
	}

	for i := 0; i < len(pulls); i++ {
		if pulls[i].Merged != nil && *pulls[i].Merged == true {
			d.LogMergedPullRequestAssignees(*pulls[i])
		}

		buffer.AppendInt(int64(*pulls[i].Base.Repo.ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*pulls[i].ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*pulls[i].Number))
		buffer.AppendByte('~')
		if pulls[i].ClosedAt == nil {
			buffer.AppendString("opened")
			buffer.AppendByte('~')
		} else {
			buffer.AppendString("closed")
			buffer.AppendByte('~')
		}
		payload, _ := json.Marshal(*pulls[i])
		_, _ = buffer.Write(escapeBytesBackslash(stripCtlAndExtFromBytes(payload)))
		buffer.AppendByte('~')
		buffer.AppendInt(1)
		buffer.AppendByte('~')
		if pulls[i].ClosedAt == nil {
			buffer.AppendInt(0)
			buffer.AppendByte('~')
		} else {
			buffer.AppendInt(1)
			buffer.AppendByte('~')
			buffer.Write([]byte(pulls[i].ClosedAt.Format(time.RFC3339Nano)))
		}
		buffer.AppendByte('\n')
	}

	issues = nil //PERF: Mark for garbage collection
	pulls = nil  //PERF: Mark for garbage collection
	sqlBuffer := bytes.NewBuffer(buffer.Bytes())
	buffer.Reset()
	buffer.Free()
	mysql.RegisterReaderHandler("data", func() io.Reader {
		return sqlBuffer
	})
	defer mysql.DeregisterReaderHandler("data")
	result, err := d.db.Exec("LOAD DATA LOCAL INFILE 'Reader::data' INTO TABLE github_events FIELDS TERMINATED BY '~' LINES TERMINATED BY '\n' (repo_id,issues_id,number,action,payload,is_pull,is_closed,closed_at)")
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("Database Insert Success", zap.Int64("Rows", rows))
	}
}

func (d *Database) BulkInsertIssues(issues []*github.Issue) {
	buffer := d.BufferPool.Get()

	for i := 0; i < len(issues); i++ {
		d.LogIssueAssignees(*issues[i])

		buffer.AppendInt(int64(*issues[i].Repository.ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*issues[i].ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*issues[i].Number))
		buffer.AppendByte('~')
		if issues[i].ClosedAt == nil {
			buffer.AppendString("opened")
			buffer.AppendByte('~')
		} else {
			buffer.AppendString("closed")
			buffer.AppendByte('~')
		}
		payload, _ := json.Marshal(*issues[i])
		_, _ = buffer.Write(escapeBytesBackslash(stripCtlAndExtFromBytes(payload)))
		buffer.AppendByte('~')
		buffer.AppendInt(0)
		buffer.AppendByte('~')
		if issues[i].ClosedAt == nil {
			buffer.AppendInt(0)
			buffer.AppendByte('~')
		} else {
			buffer.AppendInt(1)
			buffer.AppendByte('~')
			buffer.Write([]byte(issues[i].ClosedAt.Format(time.RFC3339Nano)))
		}
		buffer.AppendByte('\n')
	}

	issues = nil //PERF: Mark for garbage collection
	sqlBuffer := bytes.NewBuffer(buffer.Bytes())
	buffer.Reset()
	buffer.Free()
	mysql.RegisterReaderHandler("data", func() io.Reader {
		return sqlBuffer
	})
	defer mysql.DeregisterReaderHandler("data")
	result, err := d.db.Exec("LOAD DATA LOCAL INFILE 'Reader::data' INTO TABLE github_events FIELDS TERMINATED BY '~' LINES TERMINATED BY '\n' (repo_id,issues_id,number,action,payload,is_pull,is_closed,closed_at)")
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("Database Insert Success", zap.Int64("Rows", rows))
	}
}

func (d *Database) InsertPullRequest(pull github.PullRequest, action *string) {
	if pull.Merged != nil && *pull.Merged == true {
		d.LogMergedPullRequestAssignees(pull)
	}
	var buffer bytes.Buffer
	eventsInsert := "INSERT INTO github_events(repo_id,issues_id,action,number,payload,is_pull,is_closed,closed_at) VALUES"
	eventsValuesFmt := "(?,?,?,?,?,1,?)"
	numValues := 7

	buffer.WriteString(eventsInsert)
	buffer.WriteString(eventsValuesFmt)
	values := make([]interface{}, numValues)
	values[0] = pull.Base.Repo.ID
	values[1] = pull.ID
	values[2] = pull.Number
	values[3] = action
	payload, _ := json.Marshal(pull)
	values[4] = stripCtlAndExtFromBytes(payload)
	if pull.ClosedAt == nil {
		values[5] = false
	} else {
		values[5] = true
	}
	values[6] = pull.ClosedAt
	result, err := d.db.Exec(buffer.String(), values...)
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Debug("Database Insert Success", zap.Int64("Rows", rows))
	}
}

func (d *Database) BulkInsertPullRequests(pulls []*github.PullRequest) {
	buffer := d.BufferPool.Get()

	for i := 0; i < len(pulls); i++ {
		if pulls[i].Merged != nil && *pulls[i].Merged == true {
			d.LogMergedPullRequestAssignees(*pulls[i])
		}

		buffer.AppendInt(int64(*pulls[i].Base.Repo.ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*pulls[i].ID))
		buffer.AppendByte('~')
		buffer.AppendInt(int64(*pulls[i].Number))
		buffer.AppendByte('~')
		if pulls[i].ClosedAt == nil {
			buffer.AppendString("opened")
			buffer.AppendByte('~')
		} else {
			buffer.AppendString("closed")
			buffer.AppendByte('~')
		}
		payload, _ := json.Marshal(*pulls[i])
		_, _ = buffer.Write(escapeBytesBackslash(stripCtlAndExtFromBytes(payload)))
		buffer.AppendByte('~')
		buffer.AppendInt(1)
		buffer.AppendByte('~')
		if pulls[i].ClosedAt == nil {
			buffer.AppendInt(0)
			buffer.AppendByte('~')
		} else {
			buffer.AppendInt(1)
			buffer.AppendByte('~')
			buffer.Write([]byte(pulls[i].ClosedAt.Format(time.RFC3339Nano)))
		}
		buffer.AppendByte('\n')
	}

	pulls = nil //PERF: Mark for garbage collection
	sqlBuffer := bytes.NewBuffer(buffer.Bytes())
	buffer.Reset()
	buffer.Free()
	mysql.RegisterReaderHandler("data", func() io.Reader {
		return sqlBuffer
	})
	defer mysql.DeregisterReaderHandler("data")
	result, err := d.db.Exec("LOAD DATA LOCAL INFILE 'Reader::data' INTO TABLE github_events FIELDS TERMINATED BY '~' LINES TERMINATED BY '\n' (repo_id,issues_id,number,action,payload,is_pull,is_closed,closed_at)")
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Info("Database Insert Success", zap.Int64("Rows", rows))
	}
}

func (d *Database) LogIssueAssignees(issue github.Issue) {
	var assigneesID int64
	var buffer bytes.Buffer
	issueAssigneesInsert := "INSERT INTO github_event_assignees(repo_id,issues_id,number,is_closed,is_pull) VALUES"
	issueAssigneesValuesFmt := "(?,?,?,?,0)"
	issueAssigneesNumValues := 4

	buffer.WriteString(issueAssigneesInsert)
	buffer.WriteString(issueAssigneesValuesFmt)
	values := make([]interface{}, issueAssigneesNumValues)
	values[0] = issue.Repository.ID
	values[1] = issue.ID
	values[2] = issue.Number
	if issue.ClosedAt == nil {
		values[3] = false
	} else {
		values[3] = true
	}

	result, err := d.db.Exec(buffer.String(), values...)
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
		return
	} else {
		rows, _ := result.RowsAffected()
		assigneesID, _ = result.LastInsertId()
		utils.AppLog.Debug("Database Insert Success", zap.Int64("Rows", rows))
	}
	buffer.Reset()

	issueAssigneesLookupInsert := "INSERT INTO github_event_assignees_lk(github_event_assignees_fk, assignee) VALUES"
	issueAssigneesLookupValuesFmt := "(?,?)"
	issueAssigneesLookupNumValues := 2
	if issue.Assignees != nil && len(issue.Assignees) > 0 {
		issueAssigneesLookupNumValues = 2 * len(issue.Assignees)
	}

	buffer.WriteString(issueAssigneesLookupInsert)
	values = make([]interface{}, issueAssigneesLookupNumValues)
	if issue.Assignees != nil && len(issue.Assignees) > 0 {
		delimeter := ""
		for i := 0; i < len(issue.Assignees); i++ {
			buffer.WriteString(delimeter)
			buffer.WriteString(issueAssigneesLookupValuesFmt)
			values[i+i+0] = assigneesID
			values[i+i+1] = issue.Assignees[i].Login
			delimeter = ","
		}
	} else {
		values[0] = assigneesID
		if issue.Assignee != nil {
			values[1] = issue.Assignee.Login
		}
		buffer.WriteString(issueAssigneesLookupValuesFmt)
	}

	result, err = d.db.Exec(buffer.String(), values...)
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Debug("Database Insert Success", zap.Int64("Rows", rows))
	}
}

func (d *Database) LogMergedPullRequestAssignees(pull github.PullRequest) {
	var assigneesID int64
	var buffer bytes.Buffer
	pullAssigneesInsert := "INSERT INTO github_event_assignees(repo_id,issues_id,number,is_closed, is_pull) VALUES"
	pullAssigneesValuesFmt := "(?,?,?,?,1)"
	pullAssigneesNumValues := 4

	buffer.WriteString(pullAssigneesInsert)
	buffer.WriteString(pullAssigneesValuesFmt)
	values := make([]interface{}, pullAssigneesNumValues)
	values[0] = pull.Base.Repo.ID
	values[1] = pull.ID
	values[2] = pull.Number
	if pull.ClosedAt == nil {
		values[3] = false
	} else {
		values[3] = true
	}

	result, err := d.db.Exec(buffer.String(), values...)
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
		return
	} else {
		rows, _ := result.RowsAffected()
		assigneesID, _ = result.LastInsertId()
		utils.AppLog.Debug("Database Insert Success", zap.Int64("Rows", rows))
	}
	buffer.Reset()

	pullAssigneesLookupInsert := "INSERT INTO github_event_assignees_lk(github_event_assignees_fk, assignee) VALUES"
	pullAssigneesLookupValuesFmt := "(?,?)"
	pullAssigneesLookupNumValues := 2

	buffer.WriteString(pullAssigneesLookupInsert)
	values = make([]interface{}, pullAssigneesLookupNumValues)
	values[0] = assigneesID
	if pull.User != nil {
		values[1] = pull.User.Login
	}
	buffer.WriteString(pullAssigneesLookupValuesFmt)

	result, err = d.db.Exec(buffer.String(), values...)
	if err != nil {
		utils.AppLog.Error("Database Insert Failure", zap.Error(err))
	} else {
		rows, _ := result.RowsAffected()
		utils.AppLog.Debug("Database Insert Success", zap.Int64("Rows", rows))
	}
}

func stripCtlAndExtFromBytes(str []byte) []byte {
	b := make([]byte, len(str))
	var bl int
	for i := 0; i < len(str); i++ {
		c := str[i]
		if c >= 32 && c < 127 {
			b[bl] = c
			bl++
		}
	}
	//return b[:bl]
	str = b[:bl] //PERF
	return str
}

func escapeString(sql string) string {
	dest := make([]byte, 0, 2*len(sql))
	var escape byte
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		escape = 0
		switch c {
		case '\\':
			escape = '\\'
			break
		case '\'':
			escape = '\''
			break
		}
		if escape != 0 {
			dest = append(dest, '\\', escape)
		} else {
			dest = append(dest, c)
		}
	}
	return string(dest)
}

func escapeBytesQuotes(v []byte) []byte {
	buf := make([]byte, 2*len(v))
	pos := 0
	for _, c := range v {
		if c == '\'' {
			buf[pos] = '\''
			buf[pos+1] = '\''
			pos += 2
		} else {
			buf[pos] = c
			pos++
		}
	}
	return buf[:pos]
}

func escapeBytesBackslash(v []byte) []byte {
	buf := make([]byte, 2*len(v))
	pos := 0
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\x00':
			buf[pos] = '\\'
			buf[pos+1] = '0'
			pos += 2
		case '\n':
			buf[pos] = '\\'
			buf[pos+1] = 'n'
			pos += 2
		case '\r':
			buf[pos] = '\\'
			buf[pos+1] = 'r'
			pos += 2
		case '\x1a':
			buf[pos] = '\\'
			buf[pos+1] = 'Z'
			pos += 2
		case '\'':
			buf[pos] = '\\'
			buf[pos+1] = '\''
			pos += 2
		case '"':
			buf[pos] = '\\'
			buf[pos+1] = '"'
			pos += 2
		case '\\':
			buf[pos] = '\\'
			buf[pos+1] = '\\'
			pos += 2
		case '~': //sql delimeter
			continue
		default:
			buf[pos] = v[i]
			pos++
		}
	}
	//return buf[:pos]
	v = buf[:pos] //PERF
	return v
}
