import asyncpg
import jinja2

_env = jinja2.Environment(autoescape=True)


class TemplateNotFound(Exception):
    """Raised when a named template has no active row in notification_templates."""


async def render_template(pool: asyncpg.Pool, name: str, context: dict) -> tuple[str | None, str]:
    """Render the subject and body of the active template called name against context."""
    query = """
        SELECT subject_template, body_template
        FROM notification_templates
        WHERE name = $1 AND is_active
    """
    async with pool.acquire() as conn:
        row = await conn.fetchrow(query, name)
    if row is None:
        raise TemplateNotFound(name)

    subject = _render(row["subject_template"], context) if row["subject_template"] else None
    body = _render(row["body_template"], context)
    return subject, body


def _render(template_source: str, context: dict) -> str:
    return _env.from_string(template_source).render(context)
