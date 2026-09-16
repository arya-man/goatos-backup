import importlib.util
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('receipt', pathlib.Path(__file__).with_name('stg-runner-receipt.py'))
receipt = importlib.util.module_from_spec(spec)
spec.loader.exec_module(receipt)


class RunnerReceiptTest(unittest.TestCase):
    def test_rejects_changed_missing_and_extra_packaged_sources_and_wrong_pin(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            config = root / 'deploy/clouddeploy/stg'
            config.mkdir(parents=True)
            (root / 'assets').mkdir()
            (root / 'assets/query.json').write_text('original')
            (config / 'runner.Dockerfile').write_text('COPY assets /opt/assets\n')
            (config / 'clouddeploy.yaml').write_text('    image: pinned\n    image: pinned\n')
            proof = {'schemaVersion': 1, 'image': 'pinned', 'files': receipt.source_hashes(root)}
            receipt.check_receipt(root, proof)
            (root / 'assets/query.json').unlink()
            with self.assertRaises(ValueError): receipt.check_receipt(root, proof)
            (root / 'assets/query.json').write_text('original')
            for value in ['changed', '']:
                (root / 'assets/query.json').write_text(value)
                with self.assertRaises(ValueError): receipt.check_receipt(root, proof)
            (root / 'assets/query.json').write_text('original')
            (root / 'assets/new.json').write_text('extra')
            with self.assertRaises(ValueError): receipt.check_receipt(root, proof)
            (root / 'assets/new.json').unlink()
            (config / 'clouddeploy.yaml').write_text('    image: pinned\n    image: stale\n')
            with self.assertRaises(ValueError): receipt.check_receipt(root, proof)
            installed = root / 'image'
            (installed / 'opt/assets').mkdir(parents=True)
            (installed / 'opt/assets/query.json').write_text('stale-image')
            with self.assertRaises(ValueError): receipt.verify_image(root, installed)
            (installed / 'opt/assets/query.json').write_text('original')
            receipt.verify_image(root, installed)


if __name__ == '__main__':
    unittest.main()
