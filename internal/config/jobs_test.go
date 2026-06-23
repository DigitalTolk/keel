package config

import "testing"

func TestParseBackupJob(t *testing.T) {
	c := Default()
	yaml := []byte(`
aws:
  profile: ops
backup:
  jobs:
    prod-db:
      type: mysql
      file_prefix: app
      mysql:
        host: db1.internal
        user: backup
        db: app
        password_env: PROD_DB_PASS
      dest:
        kind: b2
        bucket: onewerx-backups
        prefix: mysqldumps/prod/
        endpoint: https://s3.eu-central-003.backblazeb2.com
        region: eu-central-003
      retention:
        keep: 7
`)
	if err := c.ApplyFile(yaml); err != nil {
		t.Fatalf("ApplyFile: %v", err)
	}

	if c.AWS.Profile != "ops" {
		t.Errorf("aws profile = %q, want ops", c.AWS.Profile)
	}
	job, ok := c.Backup.Jobs["prod-db"]
	if !ok {
		t.Fatal("job prod-db not parsed")
	}
	if job.Type != "mysql" {
		t.Errorf("type = %q, want mysql", job.Type)
	}
	if job.MySQL.Host != "db1.internal" || job.MySQL.DB != "app" || job.MySQL.PasswordEnv != "PROD_DB_PASS" {
		t.Errorf("mysql block wrong: %+v", job.MySQL)
	}
	if job.Dest.Kind != "b2" || job.Dest.Bucket != "onewerx-backups" || job.Dest.Prefix != "mysqldumps/prod/" {
		t.Errorf("dest block wrong: %+v", job.Dest)
	}
	if job.Retention.Keep != 7 {
		t.Errorf("retention keep = %d, want 7", job.Retention.Keep)
	}
}
