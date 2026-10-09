import type { useDiskSelection } from "./useDiskSelection";

export function DiskPicker({
  selection,
}: {
  selection: ReturnType<typeof useDiskSelection>;
}) {
  return (
    <fieldset class="dashboard-disk-picker">
      <legend>
        Visible disks{" "}
        <span>
          {selection.visible.length} of {selection.filesystems.length}
        </span>
      </legend>
      <p>
        Each selected disk gets its own gauge. Changes are saved in this
        browser.
      </p>
      <div class="disk-picker-options">
        {selection.filesystems.map((device) => (
          <label key={device.id}>
            <input
              type="checkbox"
              aria-label={device.name}
              checked={selection.ids.has(device.id)}
              onChange={(event) =>
                selection.choose(device, event.currentTarget.checked)
              }
            />
            <span>{device.name}</span>
          </label>
        ))}
        {selection.missing.map((device) => (
          <label key={device.id}>
            <input
              type="checkbox"
              aria-label={device.name}
              checked
              onChange={() => selection.choose(device, false)}
            />
            <span>{device.name}</span>
            <small>Unavailable</small>
          </label>
        ))}
        {!selection.filesystems.length && !selection.missing.length && (
          <p>No disks available yet.</p>
        )}
      </div>
      {selection.storageUnavailable && (
        <p role="status">
          Your choices apply for this visit; browser storage is unavailable.
        </p>
      )}
    </fieldset>
  );
}
