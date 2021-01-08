"""Tests for get_duplicate_issue and process_vulnerabilities from
parse_trivy_results.py.

This will be run as a GitHub Action when parse_trivy_results.py has been
modified.

"""
import unittest.mock as mock
from absl import flags
from absl.testing import absltest
from parse_trivy_results import get_duplicate_issue, process_vulnerabilities

FLAGS = flags.FLAGS


class TestParseTrivyResults(absltest.TestCase):
    def setUp(self):
        FLAGS.release_tag = "0.0.0"
        FLAGS.commit_id = -1

        self.mock_issue_dup = mock.Mock()
        self.mock_issue_dup.title = "Vulnerability found in release 0.0.0"
        self.mock_issue_dup.body = "body later"
        self.mock_issue_dup.number = 0

        self.mock_issue_diff = mock.Mock()
        self.mock_issue_diff.title = "Vulnerability found in release 1.1.1"
        self.mock_issue_diff.body = "body"
        self.mock_issue_diff.number = 2

        self.trivy_json_empty = dict()
        self.trivy_json_empty["Vulnerabilities"] = []

        self.trivy_json_low = {"Vulnerabilities": [
            {"Severity": "LOW"}], "Target": "Test Target"}

        self.trivy_json_high = {"Vulnerabilities": [
            {"Severity": "HIGH"}], "Target": "Test Target"}

        self.trivy_json_mixed = {"Vulnerabilities": [
            {"Severity": "LOW"}, {"Severity": "HIGH"}], "Target": "Test Target"}

        self.mock_repo = mock.Mock()
        self.mock_repo.create_issue.return_value = self.mock_issue_dup

    def test_get_duplicate_issue_no_duplicates(self):
        """Tests that None is returned when no duplicates exist in the repo."""

        self.mock_repo.get_issues.return_value = []
        existing_issue = get_duplicate_issue(self.mock_repo)
        self.assertIsNone(existing_issue)

    def test_get_duplicate_issue_one_duplicate(self):
        """Tests that the correct issue is returned when there is one duplicate."""

        self.mock_repo.get_issues.return_value = [
            self.mock_issue_diff, self.mock_issue_dup]
        existing_issue = get_duplicate_issue(self.mock_repo)
        self.assertIsNotNone(existing_issue)
        self.assertIs(existing_issue, self.mock_issue_dup)

    def test_get_duplicate_issue_two_duplicates(self):
        """Tests that the first issue is returned when there are two duplicates."""

        mock_issue_dup_earlier = mock.Mock()
        mock_issue_dup_earlier.title = "Vulnerability found in release 0.0.0"

        self.mock_repo.get_issues.return_value = [
            self.mock_issue_dup, mock_issue_dup_earlier]
        existing_issue = get_duplicate_issue(self.mock_repo)
        self.assertIsNotNone(existing_issue)
        self.assertIs(existing_issue, self.mock_issue_dup)

    def test_process_vulnerabilities_low_severity_no_dup(self):
        """Tests that an issue is created when there are no duplicates."""

        process_vulnerabilities(self.mock_repo, None,
                                self.trivy_json_low, "table")
        expected_title = "Vulnerability found in release 0.0.0"
        self.mock_repo.create_issue.assert_called_once_with(
            title=expected_title, body=mock.ANY)

    def test_process_vulnerabilities_high_severity_no_dup(self):
        """Tests that the build fails and an issue is created when there is a high severity vulnerability."""

        with self.assertRaises(SystemExit):
            process_vulnerabilities(
                self.mock_repo, None, self.trivy_json_high, "table")

        expected_title = ("Vulnerability found in release 0.0.0: "
                          "Images from commit -1 cannot be released")
        expected_in_body = ("Trivy has detected 1 vulnerabilities in your latest"
                            " build.")

        self.mock_repo.create_issue.assert_called_once()
        _, kwargs = self.mock_repo.create_issue.call_args
        self.assertEqual(len(kwargs), 2)
        self.assertEqual(kwargs["title"], expected_title)
        self.assertIn(expected_in_body, kwargs["body"])

    def test_process_vulnerabilities_mixed_severity_dup(self):
        """Tests that the build fails and an issue is created when there is a mix of severity in vulnerabilities."""

        with self.assertRaises(SystemExit):
            process_vulnerabilities(
                self.mock_repo, self.mock_issue_dup, self.trivy_json_mixed, "table")

        expected_title = ("Vulnerability found in release 0.0.0: "
                          "Images from commit -1 cannot be released")
        expected_in_body = ("Trivy has detected 2 vulnerabilities in your latest"
                            " build.")

        self.mock_issue_dup.edit.assert_called_once()
        _, kwargs = self.mock_issue_dup.edit.call_args
        self.assertEqual(len(kwargs), 2)
        self.assertEqual(kwargs["title"], expected_title)
        self.assertIn(expected_in_body, kwargs["body"])
        self.mock_repo.create_issue.assert_not_called()


if __name__ == '__main__':
    absltest.main()
