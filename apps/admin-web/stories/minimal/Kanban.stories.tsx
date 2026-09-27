import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import IconButton from "@mui/material/IconButton";
import { Iconify } from "@/components/minimal/iconify";
import { KanbanBoard, KanbanColumn, KanbanTaskCard } from "@/components/app/kanban";
import { withMinimalTheme, mobile } from "./_minimal";

const PEOPLE = [{ id: "a", name: "Anil" }, { id: "b", name: "Bhavya" }, { id: "c", name: "Chetan" }, { id: "d", name: "Divya" }];

function Board() {
  const more = <IconButton aria-label="Column actions" size="small"><Iconify icon="solar:menu-dots-bold-duotone" /></IconButton>;
  return (
    <KanbanBoard>
      <KanbanColumn title="To do" count={3} actions={more}>
        <KanbanTaskCard name="Deworm kid shed" priority="high" meta="Due 27/09/2026" comments={2} assignees={PEOPLE.slice(0, 2)} onClick={() => undefined} />
        <KanbanTaskCard name="Weigh P-02 batch" priority="medium" meta="Due 28/09/2026" attachments={1} assignees={PEOPLE.slice(2, 3)} onClick={() => undefined} />
        <KanbanTaskCard name="A very long task name that needs to wrap across two or three lines on narrow phones" priority="low" onClick={() => undefined} />
      </KanbanColumn>
      <KanbanColumn title="In progress" count={1} actions={more}>
        <KanbanTaskCard name="Isolation pen check" priority="high" selected comments={5} attachments={3} assignees={PEOPLE} onClick={() => undefined} />
      </KanbanColumn>
      <KanbanColumn title="Done" count={0} actions={more} />
    </KanbanBoard>
  );
}

const meta: Meta = { title: "Minimal/Kanban", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;
export const Columns: Story = { render: () => <Board /> };
export const ColumnsMobile: Story = { render: () => <Board />, globals: mobile };
