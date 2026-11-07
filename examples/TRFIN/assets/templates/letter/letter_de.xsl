<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:template match="/letter">
    <p>Guten Tag <xsl:value-of select="recipient"/>,</p>
  </xsl:template>
</xsl:stylesheet>
