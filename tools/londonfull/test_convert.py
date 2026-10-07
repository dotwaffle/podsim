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
            "london-full-provenance.json": '0f8c5bbe96b52c6a5e64c7a3802d97bde11059a75357d89557b09640d36cc13c',
        }.items():
            self.assertEqual(hashlib.sha256((DATA / name).read_bytes()).hexdigest(), digest)

    def test_tube_content_guard(self):
        stops = {s["id"]: s for s in self.provenance["source_stations"]}
        routes = {}
        for seq in self.provenance["sequences"]:
            route = routes.setdefault(seq["file"], {"mode": "tube", "lineId": seq["line"], "stopPointSequences": []})
            route["stopPointSequences"].append({
                "direction": seq["direction"], "branchId": seq["branch"],
                "nextBranchIds": seq["next"], "prevBranchIds": seq["previous"],
                "stopPoint": [{k: stops[s][k] for k in ("id", "name", "lat", "lon", "zone")} |
                              {"topMostParentId": stops[s]["hub"]} for s in seq["stops"]]})
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            for filename, route in routes.items():
                convert.write_json(root / filename, route | {"unused": "different bytes"})
            _, topology, _, _ = convert.topology(root, self.provenance["sources"])
            self.assertEqual(topology, self.topology)
            filename = next(iter(routes))
            for field, value in [("lat", 51.6), ("topMostParentId", "changed")]:
                with self.subTest(field=field):
                    changed = json.loads(json.dumps(routes[filename]))
                    changed["stopPointSequences"][0]["stopPoint"][0][field] = value
                    convert.write_json(root / filename, changed)
                    with self.assertRaisesRegex(ValueError, "inconsistent source|semantics changed"):
                        convert.topology(root, self.provenance["sources"])
                    convert.write_json(root / filename, routes[filename])
            changed = json.loads(json.dumps(routes[filename]))
            changed["stopPointSequences"][0]["stopPoint"].reverse()
            convert.write_json(root / filename, changed)
            with self.assertRaisesRegex(ValueError, "semantics changed"):
                convert.topology(root, self.provenance["sources"])

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


def read_od(path):
    """Return the OD pairs, their endpoints, and the band totals of a normalized CSV."""
    seen, endpoints = set(), set()
    totals = [Decimal(0)] * 8
    with path.open() as f:
        for row in csv.DictReader(f):
            key = row["from"], row["to"]
            if key in seen or key[0] == key[1]:
                raise AssertionError("duplicate or same-site pair: %s" % (key,))
            seen.add(key)
            endpoints.update(key)
            weights = [Decimal(row[b]) for b in convert.BANDS]
            if not all(w.is_finite() and w >= 0 for w in weights) or sum(weights) <= 0:
                raise AssertionError("invalid weights: %s" % (key,))
            totals = [a + b for a, b in zip(totals, weights)]
    return seen, endpoints, totals


class DLRConversionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.provenance = json.loads((DATA / "london-full-dlr-provenance.json").read_text())
        cls.topology = json.loads((DATA / "london-full-dlr.json").read_text())
        cls.tube = json.loads((DATA / "london-full-tube.json").read_text())
        cls.tube_provenance = json.loads((DATA / "london-full-provenance.json").read_text())
        cls.review = json.loads((convert.HERE / "dlr.json").read_text())

    def test_complete_snapshots(self):
        sources = json.loads((convert.HERE / "sources.json").read_text())
        self.assertEqual(self.provenance["sources"], sources)
        self.assertEqual(len(sources), 26)
        self.assertEqual(convert.tube_manifest(sources), self.tube_provenance["sources"])
        self.assertEqual({s["file"] for s in sources if "retrieved" in s}, convert.DLR_FILES)
        stops = self.provenance["dlr_source_stations"]
        self.assertEqual(len(stops), 45)
        sites = {s["id"] for s in self.topology["stations"]}
        self.assertEqual(len(sites), 310)
        self.assertEqual({s["site_id"] for s in stops} - sites, set())
        merged = {m["stop"]: m["site"] for m in self.provenance["dlr_merges"]}
        self.assertEqual(merged, {"940GZZDLBNK": "940GZZLUBNK", "940GZZDLCGT": "940GZZLUCGT",
                                  "940GZZDLSTD": "940GZZLUSTD", "940GZZDLWHM": "940GZZLUWHM"})
        for stop in stops:
            self.assertEqual(stop["site_id"], merged.get(stop["id"], stop["id"]))
        self.assertIn("940GZZDLWIQ", sites)
        self.assertEqual(self.provenance["excluded_definitions"], [])
        self.assertEqual(len(self.provenance["demand_mapping"]), 311)
        self.assertEqual({r["site_id"] for r in self.provenance["demand_mapping"]}, sites)
        self.assertEqual({r["line"] for r in self.provenance["dlr_sequences"]}, {"dlr"})

    def test_tube_part_unchanged(self):
        stations = {s["id"]: s for s in self.topology["stations"]}
        for station in self.tube["stations"]:
            self.assertEqual(stations[station["id"]], station)
        links = {(l["a"], l["b"]): l["lines"] for l in self.topology["links"]}
        tube = {(l["a"], l["b"]): l["lines"] for l in self.tube["links"]}
        self.assertEqual(len(links), 360)
        self.assertEqual({k: v for k, v in links.items() if k in tube}, tube)
        self.assertEqual({tuple(v) for k, v in links.items() if k not in tube}, {("dlr",)})
        self.assertEqual(len(links) - len(tube), 47)

    def test_all_positive_pairs_and_band_totals(self):
        seen, endpoints, totals = read_od(DATA / "london-full-dlr-od-2024.csv")
        self.assertEqual(len(seen), 76776)
        self.assertEqual(endpoints, {s["id"] for s in self.topology["stations"]})
        self.assertEqual(totals, [Decimal(v) for v in self.provenance["band_totals"]])
        self.assertEqual(totals[0], 0)
        self.assertEqual(totals[7], 0)

    def site_inputs(self):
        tube_stops = {s["id"]: {k: s[k] for k in ("id", "name", "lat", "lon", "zone")} for s in self.tube_provenance["source_stations"]}
        tube_site = {k: convert.MERGES.get(k, k) for k in tube_stops}
        tube_hubs = {k: k for k in tube_stops} | {m["site"]: m["hub"] for m in self.review["merges"]} | {"940GZZLUCYF": "HUBCAW"}
        dlr = {s["id"]: s for s in self.provenance["dlr_source_stations"]}
        dlr_stops = {k: {f: s[f] for f in ("id", "name", "lat", "lon", "zone")} for k, s in dlr.items()}
        dlr_hubs = {k: s["hub"] for k, s in dlr.items()}
        codes = {m["code"]: m["code_name"] for m in self.provenance["dlr_merges"]}
        naptan = {r["code"]: r["primary_naptan"] for r in self.provenance["demand_mapping"] if r["code"] in codes}
        explicit = {r["code"]: r["source_id"] for r in self.provenance["demand_mapping"] if r["code"] in codes and r["mixed_mode_alias"]}
        definition = (codes, naptan, set(codes), set(codes), explicit)
        return tube_site, tube_stops, tube_hubs, dlr_stops, dlr_hubs, definition

    def test_review_guards(self):
        *inputs, definition = self.site_inputs()
        site, merges = convert.dlr_sites(*inputs, self.review, definition)
        self.assertEqual(merges, self.provenance["dlr_merges"])
        self.assertEqual(site, {s["id"]: s["site_id"] for s in self.provenance["dlr_source_stations"]})
        cases = [
            ("separate", lambda r: r["separate"].pop(0), "unreviewed DLR hub"),
            ("hub", lambda r: r["merges"][0].update(hub="HUBCAW"), "hub mismatch"),
            ("code", lambda r: r["merges"][0].update(code="884"), "code mismatch"),
            ("site", lambda r: r["merges"][0].update(site="940GZZLUCYF"), "code mismatch"),
            ("name", lambda r: r["separate"][0].update(name="Incorrect"), "name mismatch"),
            ("duplicate", lambda r: r["separate"].append(r["merges"][0]), "duplicate"),
        ]
        for name, change, message in cases:
            with self.subTest(name=name):
                review = json.loads(json.dumps(self.review))
                change(review)
                with self.assertRaisesRegex(ValueError, message):
                    convert.dlr_sites(*inputs, review, definition)

    def test_dlr_alias_guards(self):
        stops = {s["id"]: s for s in self.provenance["dlr_source_stations"]}
        aliases = self.review["aliases"]
        names = {a["code"]: a["name"] for a in aliases}
        naptan = {a["code"]: a["naptan"] for a in aliases}
        self.assertEqual(convert.check_aliases(aliases, set(names), names, naptan, stops),
                         {"573": "940GZZDLWLA", "928": "940GZZDLGRE"})
        for field, value in [("name", "Incorrect"), ("naptan", "910GINCORRECT"), ("selected", "940GZZDLLEW")]:
            with self.subTest(field=field):
                changed = [a.copy() for a in aliases]
                changed[0][field] = value
                with self.assertRaisesRegex(ValueError, "alias .* mismatch"):
                    convert.check_aliases(changed, set(names), names, naptan, stops)

    def test_pinned_tube_semantics(self):
        manifest = json.loads((convert.HERE / "sources.json").read_text())
        stops = {s["id"]: {k: s[k] for k in ("id", "name", "lat", "lon", "zone")} for s in self.tube_provenance["source_stations"]}
        sequences = self.tube_provenance["sequences"]
        hubs = {s["id"]: s["hub"] for s in self.tube_provenance["source_stations"]}
        with patch.object(convert, "read_routes", return_value=(stops, hubs, sequences)):
            convert.pinned_tube(Path("unused"), manifest, self.tube_provenance)
        moved = {k: v | {"lat": v["lat"] + 0.001} if k == "940GZZLUBNK" else v for k, v in stops.items()}
        with patch.object(convert, "read_routes", return_value=(moved, hubs, sequences)):
            with self.assertRaisesRegex(ValueError, "semantics changed"):
                convert.pinned_tube(Path("unused"), manifest, self.tube_provenance)
        for changed_hubs, changed_sequences in [(hubs | {"940GZZLUBNK": "changed"}, sequences), (hubs, sequences[:-1])]:
            with patch.object(convert, "read_routes", return_value=(stops, changed_hubs, changed_sequences)):
                with self.assertRaisesRegex(ValueError, "semantics changed"):
                    convert.pinned_tube(Path("unused"), manifest, self.tube_provenance)
        with self.assertRaisesRegex(ValueError, "does not match the manifest"):
            convert.pinned_tube(Path("unused"), manifest[1:], self.tube_provenance)


if __name__ == "__main__":
    unittest.main()
