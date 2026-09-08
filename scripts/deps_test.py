import datetime as dt
import unittest
from unittest.mock import patch
import deps


class AgePolicyTests(unittest.TestCase):
    def test_exact_boundary(self):
        now = dt.datetime(2026, 9, 8, tzinfo=dt.timezone.utc)
        deps.check_age(now - dt.timedelta(days=7), now)
        for age in (dt.timedelta(days=7) - dt.timedelta(seconds=1), dt.timedelta(days=-1)):
            with self.assertRaises(ValueError):
                deps.check_age(now - age, now)

    def test_unknown_timestamp_fails_closed(self):
        for stamp in (None, dt.datetime(2020, 1, 1)):
            with self.assertRaises(ValueError):
                deps.check_age(stamp)

    def test_backdated_commit_does_not_bypass_recent_release(self):
        old = b'{"Time":"2020-01-01T00:00:00Z"}'
        recent = ('{"published_at":"' + deps.NOW.isoformat() + '"}').encode()
        with patch.object(deps, "fetch", side_effect=[old, recent]), patch.object(deps, "indexed_at", return_value=dt.datetime(2020, 1, 2, tzinfo=dt.timezone.utc)):
            with self.assertRaises(ValueError):
                deps.verify("github.com/test/backdated", "v1.0.0")

    def test_old_commit_with_recent_registry_observation_is_rejected(self):
        row = {"Path": "example.com/recent", "Version": "v1.0.0", "Timestamp": deps.NOW.isoformat()}
        with patch.object(deps, "fetch", return_value=deps.json.dumps(row).encode()), patch.object(deps, "registry_hint", return_value=dt.datetime(2020, 1, 1, tzinfo=dt.timezone.utc)):
            with self.assertRaises(ValueError):
                deps.indexed_at(row["Path"], row["Version"], dt.datetime(2020, 1, 1, tzinfo=dt.timezone.utc))
