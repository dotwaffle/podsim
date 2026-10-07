"""Offline source guard and normalized snapshot checks."""

import csv
from decimal import Decimal
import hashlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import convert

DATA = convert.HERE.parents[1] / "internal/scenarios/data"


class ConversionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.provenance = json.loads((DATA / "london-full-provenance.json").read_text())
        cls.topology = json.loads((DATA / "london-full-tube.json").read_text())

    def test_complete_snapshots(self):
        source = self.provenance["source_stations"]
        self.assertEqual(len(source), 272)
        sites = {s["id"] for s in self.topology["stations"]}
        self.assertEqual(len(sites), 269)
        self.assertEqual({s["site_id"] for s in source}, sites)
        self.assertEqual(len(self.topology["links"]), 313)
        self.assertEqual(len([r for r in self.provenance["demand_mapping"] if r["mixed_mode_alias"]]), 39)
        self.assertEqual(len(self.provenance["sources"]), 24)
        sequences = self.provenance["sequences"]
        self.assertEqual({r["direction"] for r in sequences}, {"inbound", "outbound"})
        self.assertEqual(len({r["line"] for r in sequences}), 11)
        self.assertEqual({r["file"] for r in sequences}, {s["file"] for s in self.provenance["sources"] if s["file"].endswith("bound.json")})
        for station in self.topology["stations"]:
            if station["id"] in {"940GZZNEUGST", "940GZZBPSUST"}:
                self.assertLess(station["lon"], 0)
                self.assertEqual(station["lon"], next(r["lon"] for r in source if r["id"] == station["id"]))

    def test_all_positive_pairs_and_band_totals(self):
        seen, endpoints = set(), set()
        totals = [Decimal(0)] * 8
        with (DATA / "london-full-od-2024.csv").open() as f:
            for row in csv.DictReader(f):
                key = row["from"], row["to"]
                self.assertNotIn(key, seen)
                self.assertNotEqual(*key)
                seen.add(key)
                endpoints.update(key)
                weights = [Decimal(row[b]) for b in convert.BANDS]
                self.assertTrue(all(w.is_finite() and w >= 0 for w in weights))
                self.assertGreater(sum(weights), 0)
                totals = [a + b for a, b in zip(totals, weights)]
        self.assertEqual(len(seen), 60996)
        self.assertEqual(endpoints, {s["id"] for s in self.topology["stations"]})
        self.assertEqual(totals, [Decimal(v) for v in self.provenance["band_totals"]])

    def test_central_files_unchanged(self):
        for name, digest in {
            "london-tube.json": "9a4326cd8e2c3517cf7c9a2a3a85c7e4208faeaff472cb495546d682ba6a7b66",
            "london-od-2019.csv": "76c6fec8fcea010254413dafd09cd95418596f28dd01ea7182840700c5998810",
        }.items():
            self.assertEqual(hashlib.sha256((DATA / name).read_bytes()).hexdigest(), digest)

    def test_tube_outputs_unchanged(self):
        for name, digest in {
            "london-full-tube.json": "af65234ca6be2d3280e48cf73773b574ec60c0cdd9e4473b2b3d5ad648bed139",
            "london-full-od-2024.csv": "605ac99f1e134dbd20e00401de4c671e3664b81e48e373df574927c7b60cb99c",
            "london-full-provenance.json": "f9b56e33aa48842627e319113cc35b1b50085423c9baedea4c449f2c13f70e27",
        }.items():
            self.assertEqual(hashlib.sha256((DATA / name).read_bytes()).hexdigest(), digest)

    def test_hash_guard(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            (root / "source").write_bytes(b"data")
            entry = {"file": "source", "bytes": 4, "sha256": hashlib.sha256(b"data").hexdigest()}
            convert.verify_sources(root, [entry])
            (root / "source").write_bytes(b"edit")
            with self.assertRaisesRegex(ValueError, "hash mismatch"):
                convert.verify_sources(root, [entry])

    def mapping_tables(self):
        rows = self.provenance["demand_mapping"]
        exclusions = self.provenance["excluded_definitions"]
        return [("Stations", [{}] + [{"A": r["code"], "C": r["name"]} for r in rows + exclusions]),
                ("Stn-Naptan", [{}] + [{"A": r["code"], "D": r.get("primary_naptan", r.get("naptan"))} for r in rows + exclusions]),
                ("Stn-Mode", [{}] + [{"A": r["code"], "E": "1"} for r in rows + exclusions])]

    def test_alias_guards(self):
        stops = {s["id"]: s for s in self.provenance["source_stations"]}
        aliases = json.loads((convert.HERE / "aliases.json").read_text())
        with patch.object(convert, "sheets", return_value=self.mapping_tables()):
            mapping, _, _ = convert.demand_mapping(Path("unused"), stops, aliases)
            self.assertEqual(len(mapping), 270)
        for field, value in [("name", "Incorrect"), ("naptan", "910GINCORRECT"), ("selected", "940GZZLUBNK")]:
            with self.subTest(field=field), patch.object(convert, "sheets", return_value=self.mapping_tables()):
                changed = [a.copy() for a in aliases]
                changed[0][field] = value
                with self.assertRaisesRegex(ValueError, "alias .* mismatch"):
                    convert.demand_mapping(Path("unused"), stops, changed)
        with patch.object(convert, "sheets", return_value=self.mapping_tables()):
            with self.assertRaisesRegex(ValueError, "alias count"):
                convert.demand_mapping(Path("unused"), stops, aliases[:-1])

    def test_unreviewed_exclusion(self):
        tables = self.mapping_tables()
        tables[1][1][1]["D"] = "unknown"
        stops = {s["id"]: s for s in self.provenance["source_stations"]}
        with patch.object(convert, "sheets", return_value=tables):
            with self.assertRaisesRegex(ValueError, "exclusion|mismatch"):
                convert.demand_mapping(Path("unused"), stops, json.loads((convert.HERE / "aliases.json").read_text()))

    def test_exact_aggregation_and_exclusions(self):
        rows = csv.DictReader(io.StringIO("mnlc_o,mnlc_d,tb_o,vol\n1,2,1,0.1\n3,2,1,0.2\n1,3,1,9\n4,2,1,4\n"))
        weights, counts = convert.demand(rows, {"1": "a", "2": "b", "3": "a"})
        self.assertEqual(dict(weights), {("a", "b"): [Decimal("0.3")] + [Decimal(0)] * 7})
        self.assertEqual(counts, {"source_rows": 4, "retained_rows": 2, "same_site_rows": 1, "excluded_endpoint_rows": 1})

    def test_invalid_weights_and_bands(self):
        for weight, band in [("NaN", "1"), ("Infinity", "1"), ("-1", "1"), ("0", "1"), ("1", "0"), ("1", "9")]:
            with self.subTest(weight=weight, band=band), self.assertRaises(ValueError):
                convert.demand([{"mnlc_o": "1", "mnlc_d": "2", "tb_o": band, "vol": weight}], {"1": "a", "2": "b"})

    def test_duplicate_source_band(self):
        row = {"mnlc_o": "1", "mnlc_d": "2", "tb_o": "1", "vol": "1"}
        with self.assertRaisesRegex(ValueError, "duplicate"):
            convert.demand([row, row], {"1": "a", "2": "b"})


if __name__ == "__main__":
    unittest.main()
